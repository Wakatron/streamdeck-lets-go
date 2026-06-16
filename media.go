package main

import (
	"log/slog"
)

func init() {
	slog.Info("media module loaded")
}

// mediaAction is handled inline in action.go via playerctl/wpctl
