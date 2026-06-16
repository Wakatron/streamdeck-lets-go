# streamdeck-lets-go

> A lightweight daemon for controlling Elgato Stream Deck devices with a built-in web UI.

---

## Features

- **Web-based editor** — Alpine.js SPA for managing pages, keys, icons, and actions
- **Multi-action support** — assign shell commands, scripts, page switching, media/volume/brightness controls, and **keyboard shortcuts** to any key
- **On-device display** — render icons, text labels, and periodic command output directly on Stream Deck keys
- **Auto page switching** — automatically change pages based on the focused window (Hyprland, Sway, Niri, GNOME, KDE, X11)
- **Screensaver** — dim or blank the deck after a configurable idle timeout
- **Hot-reload** — config changes via the web UI are applied live; manual edits to `config.json` are detected and reloaded automatically
- **No cloud, no Electron** — single Go binary with an embedded web frontend

---

## Quick Start

### Prerequisites

- Linux with **CGO** enabled (required by `bearsh/hid` / libusb)
- Stream Deck connected via USB

### Install from source

```bash
go build -o streamdeck-lets-go .
```

### Run

```bash
# Full daemon (deck hardware + web UI)
./streamdeck-lets-go daemon

# Web UI only (config editor without deck)
./streamdeck-lets-go serve -addr :9090

# List connected devices
./streamdeck-lets-go discover
```

Open `http://localhost:9090` in your browser.

---

## Configuration

The config file is stored at `~/.config/streamdeck-lets-go/config.json`. You can edit it manually or through the web UI.

### Key actions

| Type | Field | Description |
|---|---|---|
| `command` | `command` | Run a shell command |
| `script` | `script` | Run an executable script |
| `builtin` | `builtin` | Built-in media/volume/brightness controls |
| `page` | `page` | Switch to another page |
| `keyboard` | `keys` | Send a keyboard shortcut (e.g. `ctrl+t`, `super+return`) |

### Keyboard actions

Sends a key combination to the currently focused window.

| Platform | Tool required |
|---|---|
| Wayland | `wtype` |
| X11 | `xdotool` |

The daemon warns on startup if the required tool is missing.

### Built-in actions

| Value | Action |
|---|---|
| `volume_up` / `volume_down` / `volume_mute` | PipeWire volume (via `wpctl`) |
| `brightness_up` / `brightness_down` | Display brightness (via `brightnessctl`) |
| `media_play_pause` / `media_next` / `media_prev` / `media_stop` | MPRIS media (via `playerctl`) |

### Periodic display

Any key can display the output of a command or script, updated on an interval. Use this for weather, system stats, calendar, etc.

### Auto-switch rules

Automatically change pages based on the focused window:

```json
{
  "wm_class": "firefox",
  "page": "browser"
}
```

Supported compositors: Hyprland, Sway, Niri, GNOME, KDE, X11.

### Screensaver

After a configurable idle period the deck dims or shows a custom image.

---

## Building from source

```bash
git clone git@git.totmin.ru:en2zmax/streamdeck-lets-go.git
cd streamdeck-lets-go
go build -o streamdeck-lets-go .
```

Dependencies:

| Package | Purpose |
|---|---|
| `libusb-1.0` | HID communication via CGO |
| `librsvg` | SVG icon rendering (optional) |
| `fontconfig` | System font detection |

---

## Supported devices

| Model | PID | Keys |
|---|---|---|
| Stream Deck Original | `0x006d` | 15 |
| Stream Deck Mini | `0x0063` | 6 |
| Stream Deck XL | `0x006c` | 32 |
| Stream Deck MK.2 | `0x0080` | 15 |
| Stream Deck Pedal | `0x0086` | 3 |

---

## License

MIT
