package main

import (
	"log/slog"
	"time"

	"streamdeck-lets-go/internal/config"
)

type Screensaver struct {
	enabled    bool
	idleAfter  time.Duration
	lastInput  time.Time
	active     bool
	savedBrightness int
}

func NewScreensaver(cfg *config.ScreensaverCfg) *Screensaver {
	ss := &Screensaver{
		enabled:   cfg.Enabled,
		idleAfter: time.Duration(cfg.IdleSeconds) * time.Second,
		lastInput: time.Now(),
	}
	if ss.idleAfter <= 0 {
		ss.idleAfter = 30 * time.Second
	}
	return ss
}

func (ss *Screensaver) NotifyInput() {
	ss.lastInput = time.Now()
	if ss.active {
		ss.active = false
		slog.Debug("screensaver deactivated")
	}
}

func (ss *Screensaver) Check() bool {
	if !ss.enabled {
		return false
	}
	if time.Since(ss.lastInput) > ss.idleAfter {
		if !ss.active {
			ss.active = true
			slog.Debug("screensaver activated")
			return true
		}
		return false
	}
	return false
}

func (ss *Screensaver) Activate(deck *Deck, cfg *config.ScreensaverCfg) {
	ss.savedBrightness = deck.Brightness()

	brightness := cfg.Brightness
	if brightness <= 0 {
		brightness = 10
	}
	if err := deck.SetBrightness(brightness); err != nil {
		slog.Warn("screensaver: set brightness", "error", err)
	}

	if cfg.Image != "" {
		img, err := loadImage(cfg.Image, 0, 0)
		if err != nil {
			slog.Warn("screensaver: load image", "path", cfg.Image, "error", err)
			return
		}
		if err := deck.FillPanel(img); err != nil {
			slog.Warn("screensaver: fill panel", "error", err)
		}
	}

	slog.Info("screensaver activated")
}

func (ss *Screensaver) Deactivate(deck *Deck) {
	brightness := ss.savedBrightness
	if brightness <= 0 {
		brightness = 75
	}
	if err := deck.SetBrightness(brightness); err != nil {
		slog.Warn("screensaver: restore brightness", "error", err)
	}
	ss.savedBrightness = 0
	slog.Info("screensaver deactivated")
}

func (ss *Screensaver) IsActive() bool {
	return ss.active
}
