package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	startPage := fs.String("page", "", "page to activate on startup (default: config's default_page)")
	noDeck := fs.Bool("no-deck", false, "run without Stream Deck hardware (config editing only)")
	verbose := fs.Bool("v", false, "verbose output")

	if len(os.Args) < 2 || os.Args[1] == "--help" || os.Args[1] == "-help" || os.Args[1] == "-h" {
		fmt.Fprintf(os.Stderr, "Usage: streamdeck-lets-go [flags] [command]\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  daemon   run the Stream Deck daemon with web UI (default)\n")
		fmt.Fprintf(os.Stderr, "  switch   switch running daemon to a page (e.g. 'switch games')\n")
		fmt.Fprintf(os.Stderr, "  discover list connected Stream Decks\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		fs.PrintDefaults()
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "daemon":
		fs.Parse(args)
	case "switch":
		fs.Parse(args)
		if fs.NArg() == 0 {
			fmt.Fprintf(os.Stderr, "Usage: streamdeck-lets-go switch <page>\n")
			os.Exit(1)
		}
		if err := switchToPage(*httpAddr, fs.Arg(0)); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("switched to page %q\n", fs.Arg(0))
		return
	case "discover":
		discover()
		return
	default:
		if cmd == "serve" {
			fmt.Fprintf(os.Stderr, "streamdeck-lets-go: 'serve' removed; use 'daemon --no-deck' instead\n")
			os.Exit(1)
		}
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
		StartPage:   *startPage,
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

// switchToPage tells the running daemon (at httpAddr) to activate the given page
// by POSTing to its /api/activate-page endpoint.
func switchToPage(httpAddr, page string) error {
	host := httpAddr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	url := "http://" + host + "/api/activate-page"

	body, _ := json.Marshal(map[string]string{"page": page})
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("daemon not running? %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("switch failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}
