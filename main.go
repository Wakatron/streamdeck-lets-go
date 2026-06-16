package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"streamdeck-lets-go/internal/config"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	fs := flag.NewFlagSet("streamdeck-lets-go", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config.json (default: ~/.config/streamdeck-lets-go/config.json)")
	httpAddr := fs.String("addr", ":9090", "web UI listen address")
	noDeck := fs.Bool("no-deck", false, "run without Stream Deck hardware (config editing only)")
	verbose := fs.Bool("v", false, "verbose output")

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: streamdeck-lets-go [flags] [command]\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  daemon  run the Stream Deck daemon with web UI (default)\n")
		fmt.Fprintf(os.Stderr, "  serve   run web UI only (no deck hardware)\n")
		fmt.Fprintf(os.Stderr, "  discover  list connected Stream Decks\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		fs.PrintDefaults()
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "daemon":
		fs.Parse(args)
	case "serve":
		fs.Parse(args)
		*noDeck = true
	case "discover":
		discover()
		return
	default:
		fs.Parse(os.Args[1:])
	}

	if *verbose {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	httpEnabled := true
	if err := Run(ctx, cfg, RunOptions{
		ConfigPath:  *configPath,
		HTTPAddr:    *httpAddr,
		HTTPEnabled: httpEnabled,
		NoDeck:      *noDeck,
	}); err != nil {
		slog.Error("run", "error", err)
		os.Exit(1)
	}
}

func discover() {
	devices, err := EnumerateDevices()
	if err != nil {
		slog.Error("discover", "error", err)
		os.Exit(1)
	}
	if len(devices) == 0 {
		fmt.Println("No Stream Deck devices found.")
		return
	}
	fmt.Printf("Found %d Stream Deck(s):\n", len(devices))
	for _, d := range devices {
		fmt.Printf("  Serial: %s  Model: %s  PID: 0x%04x\n", d.Serial, d.Model, d.PID)
	}
}
