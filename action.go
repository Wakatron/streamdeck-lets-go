package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"streamdeck-lets-go/internal/config"
)

func ExecuteAction(a *config.Action, deck *Deck, pm *PageManager) error {
	if a == nil {
		return nil
	}

	slog.Debug("execute action", "type", a.Type)

	switch a.Type {
	case "command":
		return execCommand(a)
	case "builtin":
		return execBuiltin(a, deck, pm)
	case "script":
		return execScript(a)
	case "page":
		if pm != nil {
			return pm.ActivatePage(a.Page)
		}
		return fmt.Errorf("page manager not available")
	case "keyboard":
		return execKeyboard(a)
	default:
		return fmt.Errorf("unknown action type: %s", a.Type)
	}
}

func execCommand(a *config.Action) error {
	cmd := exec.Command("sh", "-c", a.Command)

	if a.Background {
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start command: %w", err)
		}
		go func() {
			if err := cmd.Wait(); err != nil {
				slog.Warn("command finished", "cmd", a.Command, "error", err)
			}
		}()
		return nil
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run command: %w", err)
	}
	return nil
}

func execScript(a *config.Action) error {
	cmd := exec.Command(a.Script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run script: %w", err)
	}
	return nil
}

func execDisplayCapture(d *config.DisplayCfg, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var cmd *exec.Cmd
	if d.Command != "" {
		cmd = exec.CommandContext(ctx, "sh", "-c", d.Command)
	} else {
		cmd = exec.CommandContext(ctx, d.Script)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}
	return output, err
}

func execBuiltin(a *config.Action, deck *Deck, pm *PageManager) error {
	parts := strings.SplitN(a.Builtin, ":", 2)
	if len(parts) < 1 {
		return fmt.Errorf("invalid builtin: %s", a.Builtin)
	}

	category := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	slog.Debug("builtin", "category", category, "action", action)

	switch category {
	case "media":
		return mediaAction(action)
	case "volume":
		return volumeAction(action)
	case "brightness":
		return brightnessAction(action)
	case "page":
		if pm != nil {
			return pageBuiltinAction(action, pm)
		}
		return fmt.Errorf("page manager not available")
	case "deck":
		return deckBuiltinAction(action, deck)
	default:
		return fmt.Errorf("unknown builtin category: %s", category)
	}
}

func mediaAction(action string) error {
	var cmd string
	switch action {
	case "playpause":
		cmd = "playerctl play-pause"
	case "next":
		cmd = "playerctl next"
	case "prev":
		cmd = "playerctl previous"
	case "stop":
		cmd = "playerctl stop"
	default:
		return fmt.Errorf("unknown media action: %s", action)
	}
	return exec.Command("sh", "-c", cmd).Run()
}

func volumeAction(action string) error {
	var cmd string
	switch action {
	case "up":
		cmd = "wpctl set-volume @DEFAULT_AUDIO_SINK@ 5%+"
	case "down":
		cmd = "wpctl set-volume @DEFAULT_AUDIO_SINK@ 5%-"
	case "mute":
		cmd = "wpctl set-mute @DEFAULT_AUDIO_SINK@ toggle"
	default:
		return fmt.Errorf("unknown volume action: %s", action)
	}
	return exec.Command("sh", "-c", cmd).Run()
}

func brightnessAction(action string) error {
	var cmd string
	switch action {
	case "up":
		cmd = "brightnessctl s +10%"
	case "down":
		cmd = "brightnessctl s 10%-"
	default:
		return fmt.Errorf("unknown brightness action: %s", action)
	}
	return exec.Command("sh", "-c", cmd).Run()
}

func pageBuiltinAction(action string, pm *PageManager) error {
	switch action {
	case "next":
		pageNames := pm.PageNames()
		current := pm.ActivePageName()
		for i, name := range pageNames {
			if name == current {
				next := (i + 1) % len(pageNames)
				return pm.ActivatePage(pageNames[next])
			}
		}
		if len(pageNames) > 0 {
			return pm.ActivatePage(pageNames[0])
		}
	case "prev":
		pageNames := pm.PageNames()
		current := pm.ActivePageName()
		for i, name := range pageNames {
			if name == current {
				prev := (i - 1 + len(pageNames)) % len(pageNames)
				return pm.ActivatePage(pageNames[prev])
			}
		}
		if len(pageNames) > 0 {
			return pm.ActivatePage(pageNames[0])
		}
	default:
		return fmt.Errorf("unknown page action: %s", action)
	}
	return nil
}

func deckBuiltinAction(action string, deck *Deck) error {
	switch action {
	case "brightness-up":
		current := deck.Brightness()
		next := current + 25
		if next > 100 {
			next = 25
		}
		return deck.SetBrightness(next)
	case "brightness-down":
		current := deck.Brightness()
		next := current - 25
		if next < 25 {
			next = 100
		}
		return deck.SetBrightness(next)
	default:
		return fmt.Errorf("unknown deck action: %s", action)
	}
}

func keyboardTool() string {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if _, err := exec.LookPath("ydotool"); err == nil {
			return "ydotool"
		}
		if _, err := exec.LookPath("wtype"); err == nil {
			return "wtype"
		}
		return ""
	}
	if _, err := exec.LookPath("xdotool"); err == nil {
		return "xdotool"
	}
	return ""
}

func execKeyboard(a *config.Action) error {
	tool := keyboardTool()
	if tool == "" {
		wayland := os.Getenv("WAYLAND_DISPLAY") != ""
		if wayland {
			return fmt.Errorf("keyboard action requires wtype — install it (e.g. apk add wtype) and restart")
		}
		return fmt.Errorf("keyboard action requires xdotool — install it (e.g. apk add xdotool) and restart")
	}

	keys := strings.ToLower(strings.TrimSpace(a.Keys))
	if keys == "" {
		return fmt.Errorf("keyboard action: keys is empty")
	}

	parts := strings.Split(keys, "+")
	if len(parts) == 0 {
		return fmt.Errorf("keyboard action: invalid keys format %q", a.Keys)
	}

	switch tool {
	case "ydotool":
		return execYDOTool(keys)
	case "wtype":
		return execWType(parts)
	case "xdotool":
		return execXDoTool(keys)
	default:
		return fmt.Errorf("keyboard action: unsupported tool %q", tool)
	}
}

func execWType(parts []string) error {
	mainKey := parts[len(parts)-1]
	mods := parts[:len(parts)-1]

	args := make([]string, 0, 2+len(mods)*2)

	for _, m := range mods {
		args = append(args, "-M", m)
	}
	args = append(args, "-P", mainKey, "-p", mainKey)
	for i := len(mods) - 1; i >= 0; i-- {
		args = append(args, "-m", mods[i])
	}

	cmd := exec.Command("wtype", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func execXDoTool(keys string) error {
	cmd := exec.Command("xdotool", "key", keys)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func execYDOTool(keys string) error {
	// Use 'type' command instead of 'key' for better compatibility with Wine/games on Wayland
	cmd := exec.Command("ydotool", "type", keys)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (pm *PageManager) PageNames() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	names := make([]string, 0, len(pm.pages))
	for name := range pm.pages {
		names = append(names, name)
	}
	return names
}
