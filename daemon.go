package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"streamdeck-lets-go/internal/config"
)

type RunOptions struct {
	ConfigPath  string
	HTTPAddr    string
	HTTPEnabled bool
	NoDeck      bool
}

func Run(ctx context.Context, cfg *config.Config, opts RunOptions) error {
	web := NewWebServer(cfg, opts.ConfigPath)

	if !opts.NoDeck || opts.HTTPEnabled {
		checkKeyboardTool()
	}

	if opts.HTTPEnabled {
		go func() {
			if err := web.Serve(ctx, opts.HTTPAddr); err != nil {
				slog.Error("web server error", "error", err)
			}
		}()
	}

	if opts.NoDeck {
		<-ctx.Done()
		return nil
	}

	var deck *Deck
	var err error

	for {
		deck, err = OpenDeck("")
		if err == nil {
			break
		}
		slog.Warn("no stream deck found, retrying in 5s", "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	defer deck.Close()

	deck.SetBrightness(deviceBrightness(cfg))

	pm := NewPageManager(deck)
	pm.LoadPages(cfg.Pages)
	web.SetPageManager(pm)

	if err := pm.ActivatePage(cfg.DefaultPage); err != nil {
		slog.Warn("activate default page", "error", err)
	}
	pm.startPeriodicKeys()

	var windowCh <-chan Window
	var detector WindowDetector

	defer func() {
		if detector != nil {
			detector.Close()
		}
	}()

	if len(cfg.AutoSwitch) > 0 {
		detector = NewWindowDetector()
		windowCh, _ = detector.Start(ctx)
	}

	asm := NewAutoSwitchManager(cfg.AutoSwitch, cfg.DefaultPage)

	ssCtrl := NewScreensaver(&cfg.Screensaver)

	reconnectTicker := time.NewTicker(5 * time.Second)
	defer reconnectTicker.Stop()

	ssTicker := time.NewTicker(5 * time.Second)
	defer ssTicker.Stop()

	configTicker := time.NewTicker(3 * time.Second)
	defer configTicker.Stop()
	var lastConfigMod time.Time

	slog.Info("daemon started")
	defer slog.Info("daemon stopped")

	for {
		select {
		case evt, ok := <-deck.Events():
			if !ok {
				slog.Warn("deck event channel closed, attempting reconnect...")
				deck.Close()
				deck = reconnectDeck(ctx, cfg, &pm, asm)
				if deck == nil {
					return ctx.Err()
				}
				deck.SetBrightness(deviceBrightness(cfg))
				continue
			}

			wasSsActive := ssCtrl.IsActive()
			ssCtrl.NotifyInput()

			if evt.Kind == EventKeyPressed {
				if wasSsActive {
					ssCtrl.Deactivate(deck)

					savedOutputs := pm.GetDisplayOutputs()

					if page := pm.ActivePage(); page != nil {
						if err := pm.ActivatePage(pm.ActivePageName()); err != nil {
							slog.Warn("screensaver: re-render page", "error", err)
						}
					}

					for idx, output := range savedOutputs {
						pm.ReRenderDisplayKey(idx, output)
					}
				}

				page := pm.ActivePage()
				if page == nil {
					continue
				}
				for _, k := range page.Keys {
					if k.Index == evt.Index && k.Action != nil {
						slog.Debug("key pressed", "index", evt.Index, "action", k.Action.Type)

						if k.Action.Type == "page" {
							asm.NotifyManualPage(k.Action.Page)
						}

						go func(a *config.Action) {
							if err := ExecuteAction(a, deck, pm); err != nil {
								slog.Error("execute action", "error", err)
							}
						}(k.Action)
						break
					}
				}
			}

		case win := <-windowCh:
			if page, ok := asm.Evaluate(win, pm.ActivePageName()); ok {
				if err := pm.ActivatePage(page); err != nil {
					slog.Warn("auto-switch: activate page", "error", err)
				}
				pm.startPeriodicKeys()
			}

		case <-configTicker.C:
			path := 	config.ConfigPath(opts.ConfigPath)
			fi, err := os.Stat(path)
			if err != nil {
				continue
			}
			mt := fi.ModTime()
			if mt.After(lastConfigMod) && !lastConfigMod.IsZero() {
				lastConfigMod = mt
				slog.Info("config file changed, reloading")
				newCfg, err := config.LoadConfig(opts.ConfigPath)
				if err != nil {
					slog.Error("reload config", "error", err)
					continue
				}
				cfg = newCfg
				web.UpdateConfig(cfg)
				ssCtrl = NewScreensaver(&cfg.Screensaver)
				deck.SetBrightness(deviceBrightness(cfg))
				pm.stopPeriodicKeys()
				pm.LoadPages(cfg.Pages)
				asm.Reload(cfg.AutoSwitch)
				if len(cfg.AutoSwitch) > 0 && detector == nil {
					detector = NewWindowDetector()
					windowCh, _ = detector.Start(ctx)
				}
				if err := pm.ActivatePage(cfg.DefaultPage); err != nil {
					slog.Warn("reload: activate default page", "error", err)
				}
				pm.startPeriodicKeys()
			}
			if lastConfigMod.IsZero() {
				lastConfigMod = mt
			}

		case <-reconnectTicker.C:
			if err := deck.SetBrightness(deck.Brightness()); err != nil {
				slog.Warn("deck connection lost, reconnecting...", "error", err)
				deck.Close()
				deck = reconnectDeck(ctx, cfg, &pm, asm)
				if deck == nil {
					return ctx.Err()
				}
				deck.SetBrightness(deviceBrightness(cfg))
			}

		case <-ssTicker.C:
			if ssCtrl.Check() {
				ssCtrl.Activate(deck, &cfg.Screensaver)
			}

		case <-ctx.Done():
			return nil
		}
	}
}

func checkKeyboardTool() {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if _, err := exec.LookPath("wtype"); err != nil {
			slog.Warn("keyboard actions require wtype on Wayland — install it (e.g. apk add wtype) and restart")
		}
	} else {
		if _, err := exec.LookPath("xdotool"); err != nil {
			slog.Warn("keyboard actions require xdotool on X11 — install it (e.g. apk add xdotool) and restart")
		}
	}
}

func deviceBrightness(cfg *config.Config) int {
	if len(cfg.Devices) > 0 && cfg.Devices[0].Brightness > 0 {
		return cfg.Devices[0].Brightness
	}
	return 75
}

func reconnectDeck(ctx context.Context, cfg *config.Config, pm **PageManager, asm *AutoSwitchManager) *Deck {
	for {
		newDeck, err := OpenDeck("")
		if err == nil {
			*pm = NewPageManager(newDeck)
			(*pm).LoadPages(cfg.Pages)
			(*pm).ActivatePage(cfg.DefaultPage)
			(*pm).startPeriodicKeys()
			asm.NotifyManualPage(cfg.DefaultPage)
			return newDeck
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
		}
	}
}
