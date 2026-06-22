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

	var decks []*Deck
	var err error

	for {
		decks, err = OpenAllDecks()
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

	pageMgrs := make([]*PageManager, len(decks))
	for i, d := range decks {
		pageMgrs[i] = NewPageManager(d)
		pageMgrs[i].defaultFont = cfg.Font
		pageMgrs[i].showLabelBackground = cfg.ShowLabelBackground
		pageMgrs[i].LoadPages(cfg.Pages)
	}
	primaryPM := pageMgrs[0]
	primaryDeck := decks[0]

	defer func() {
		for _, d := range decks {
			d.Close()
		}
	}()

	for _, d := range decks {
		d.SetBrightness(deviceBrightness(cfg, d.Serial()))
	}

	web.SetDecks(decks)
	web.SetPageManager(primaryPM)
	web.SetExtraPageManagers(pageMgrs[1:])

	for _, pm := range pageMgrs {
		if err := pm.ActivatePage(cfg.DefaultPage); err != nil {
			slog.Warn("activate default page", "error", err)
		}
		pm.startPeriodicKeys()
	}

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

	ge := NewGestureEngine(cfg.Timing.LongPressMs, cfg.Timing.DoubleTapMs, func(idx int, a *config.Action) {
		if a == nil {
			return
		}
		oldPage := primaryPM.ActivePageName()
		if err := ExecuteAction(a, primaryDeck, primaryPM); err != nil {
			slog.Error("execute action", "error", err)
		}
		newPage := primaryPM.ActivePageName()
		if newPage != oldPage {
			if a.Type == "page" {
				asm.NotifyManualPage(a.Page)
			}
			for _, pm := range pageMgrs {
				pm.stopPeriodicKeys()
				pm.startPeriodicKeys()
			}
			web.BroadcastPageChange(newPage)
		} else {
			primaryPM.RefreshDisplayKey(idx)
		}
	})

	reconnectTicker := time.NewTicker(5 * time.Second)
	defer reconnectTicker.Stop()

	ssTicker := time.NewTicker(5 * time.Second)
	defer ssTicker.Stop()

	configTicker := time.NewTicker(3 * time.Second)
	defer configTicker.Stop()
	var lastConfigMod time.Time

	slog.Info("daemon started", "decks", len(decks))
	defer slog.Info("daemon stopped")

	for {
		select {
		case evt, ok := <-primaryDeck.Events():
			if !ok {
				slog.Warn("deck event channel closed, attempting reconnect...")

				// Stop all periodic scripts to avoid wasting cycles on a closed device.
				primaryPM.stopPeriodicKeys()
				asm.Pause()

				primaryDeck.Close()
				newDeck := reconnectDeck(ctx, cfg, &primaryPM)
				if newDeck == nil {
					return ctx.Err()
				}

				// Update all references to the new device and page manager.
				pageMgrs[0] = primaryPM
				decks[0] = newDeck
				primaryDeck = newDeck
				web.SetDecks(decks)
				web.SetPageManager(primaryPM)

				newDeck.SetBrightness(deviceBrightness(cfg, newDeck.Serial()))
				asm.NotifyManualPage(cfg.DefaultPage)
				asm.Resume()
				continue
			}

			wasSsActive := ssCtrl.IsActive()
			ssCtrl.NotifyInput()

			if evt.Kind == EventKeyPressed {
				if wasSsActive {
					ssCtrl.Deactivate(primaryDeck)

					savedOutputs := primaryPM.GetDisplayOutputs()

					if page := primaryPM.ActivePage(); page != nil {
						for _, pm := range pageMgrs {
							if err := pm.ActivatePage(primaryPM.ActivePageName()); err != nil {
								slog.Warn("screensaver: re-render page", "error", err)
							}
						}
					}

					for idx, dout := range savedOutputs {
						if dout != nil {
							primaryPM.ReRenderDisplayKey(idx, dout.Text)
						}
					}
				}
			}

			page := primaryPM.ActivePage()
			if page == nil {
				continue
			}
			for _, k := range primaryPM.activeKeys() {
				if k.Index == evt.Index {
					if len(k.Actions) > 0 {
						ge.HandleEvent(evt, k.Actions)
					}
					break
				}
			}

		case win := <-windowCh:
			if asm.IsPaused() {
				// Drain buffered window events accumulated while disconnected.
				for len(windowCh) > 0 {
					<-windowCh
				}
				continue
			}
			if page, ok := asm.Evaluate(win, primaryPM.ActivePageName()); ok {
				for _, pm := range pageMgrs {
					if err := pm.ActivatePage(page); err != nil {
						slog.Warn("auto-switch: activate page", "error", err)
					}
					pm.stopPeriodicKeys()
					pm.startPeriodicKeys()
				}
				web.BroadcastPageChange(page)
			}

		case <-configTicker.C:
			path := config.ConfigPath(opts.ConfigPath)
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
				for _, d := range decks {
					d.SetBrightness(deviceBrightness(cfg, d.Serial()))
				}
			var activePages []string
			for _, pm := range pageMgrs {
				activePages = append(activePages, pm.ActivePageName())
				pm.stopPeriodicKeys()
				pm.defaultFont = cfg.Font
				pm.showLabelBackground = cfg.ShowLabelBackground
				pm.LoadPages(cfg.Pages)
			}
				ge.ReloadTiming(cfg.Timing.LongPressMs, cfg.Timing.DoubleTapMs)
				asm.Reload(cfg.AutoSwitch)
				if len(cfg.AutoSwitch) > 0 && detector == nil {
					detector = NewWindowDetector()
					windowCh, _ = detector.Start(ctx)
				}
				for i, pm := range pageMgrs {
					page := cfg.DefaultPage
					if i < len(activePages) && activePages[i] != "" {
						for _, p := range cfg.Pages {
							if p.Name == activePages[i] {
								page = activePages[i]
								break
							}
						}
					}
					if err := pm.ActivatePage(page); err != nil {
						slog.Warn("reload: activate page", "error", err)
					}
					pm.startPeriodicKeys()
				}
			}
			if lastConfigMod.IsZero() {
				lastConfigMod = mt
			}

		case <-reconnectTicker.C:
			if err := primaryDeck.SetBrightness(primaryDeck.Brightness()); err != nil {
				slog.Warn("deck connection lost, reconnecting...", "error", err)

				// Stop all periodic scripts to avoid wasting cycles on a closed device.
				primaryPM.stopPeriodicKeys()
				asm.Pause()

				primaryDeck.Close()
				newDeck := reconnectDeck(ctx, cfg, &primaryPM)
				if newDeck == nil {
					return ctx.Err()
				}

				// Update all references to the new device and page manager.
				pageMgrs[0] = primaryPM
				decks[0] = newDeck
				primaryDeck = newDeck
				web.SetDecks(decks)
				web.SetPageManager(primaryPM)

				newDeck.SetBrightness(deviceBrightness(cfg, newDeck.Serial()))
				asm.NotifyManualPage(cfg.DefaultPage)
				asm.Resume()
			}

		case <-ssTicker.C:
			if ssCtrl.Check() {
				for _, d := range decks {
					ssCtrl.Activate(d, &cfg.Screensaver)
				}
			}

		case <-ctx.Done():
			return nil
		}
	}
}

func checkKeyboardTool() {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		// On Wayland, prefer ydotool over wtype
		if _, err := exec.LookPath("ydotool"); err == nil {
			// Start ydotoold daemon if not already running
			cmd := exec.Command("ydotoold")
			cmd.Stdout = nil
			cmd.Stderr = nil
			// Run in background, don't wait
			_ = cmd.Start()
			slog.Info("ydotoold daemon started for keyboard input")
		} else if _, err := exec.LookPath("wtype"); err == nil {
			slog.Warn("ydotool not found, falling back to wtype — consider installing ydotool for better Wayland support (e.g. apk add ydotool)")
		} else {
			slog.Warn("keyboard actions require ydotool or wtype on Wayland — install one (e.g. apk add ydotool) and restart")
		}
	} else {
		if _, err := exec.LookPath("xdotool"); err != nil {
			slog.Warn("keyboard actions require xdotool on X11 — install it (e.g. apk add xdotool) and restart")
		}
	}
}

func deviceBrightness(cfg *config.Config, serial string) int {
	for _, d := range cfg.Devices {
		if d.Serial == serial || d.Serial == "" {
			if d.Brightness > 0 {
				return d.Brightness
			}
			return 75
		}
	}
	return 75
}

func reconnectDeck(ctx context.Context, cfg *config.Config, pm **PageManager) *Deck {
	for {
		newDeck, err := OpenDeck("")
		if err == nil {
			*pm = NewPageManager(newDeck)
			(*pm).defaultFont = cfg.Font
			(*pm).showLabelBackground = cfg.ShowLabelBackground
			(*pm).LoadPages(cfg.Pages)
			(*pm).ActivatePage(cfg.DefaultPage)
			(*pm).startPeriodicKeys()
			return newDeck
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
		}
	}
}
