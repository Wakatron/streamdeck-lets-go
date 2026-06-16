# streamdeck-lets-go

> A lightweight daemon for controlling Elgato Stream Deck devices with a built-in web UI.

---

## Features

- **Web-based editor** — Alpine.js SPA for managing pages, keys, icons, and actions
- **Multi-action keys** — assign different actions to **tap**, **long press**, **double tap**, and **hold** on the same key
- **On-device display** — render icons, text labels, and periodic command output directly on Stream Deck keys; output can include custom background and text colors
- **Auto page switching** — automatically change pages based on the focused window (Hyprland, Sway, Niri, GNOME, KDE, X11)
- **Screensaver** — dim or blank the deck after a configurable idle timeout
- **Gesture timing** — configurable long press (default 500ms) and double tap (default 300ms) thresholds
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

### Multi-action keys

Each key can have multiple actions with different triggers:

```json
{
  "index": 7,
  "icon": "fa:chevron-right",
  "label": "Next",
  "actions": [
    { "trigger": "tap", "type": "builtin", "builtin": "page:next" },
    { "trigger": "long_press", "type": "builtin", "builtin": "page:prev" }
  ]
}
```

Supported triggers:

| Trigger | Behavior |
|---|---|
| `tap` | Quick press and release |
| `long_press` | Held past the threshold (default 500ms) |
| `double_tap` | Two taps within the threshold (default 300ms) |
| `hold_start` | Fires when held past the threshold |
| `hold_end` | Fires on release after a hold |

### Key action types

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
| `page:next` / `page:prev` | Switch to next/previous page |
| `deck:brightness-up` / `deck:brightness-down` | Cycle deck brightness |

### Periodic display

Any key can display the output of a command or script, updated on an interval. Use this for weather, system stats, monitoring, etc.

The command output can include **background color** and **text color** using one of these formats:

**JSON format:**
```json
{"text": "Server OK", "background": "#22c55e", "text_color": "#ffffff"}
```

**First-line-as-color format:**
```
#ff0000
Server down!
```

If no color is specified, the key uses its configured background or the default black.

### Gesture timing

Configured in the web UI (Settings → Gesture Timing) or in `config.json`:

```json
"timing": {
  "long_press_ms": 500,
  "double_tap_ms": 300
}
```

### Default font

Set globally in Settings → Device → Default Font (`"medium"` or `"regular"`), or override per-key in the key editor.

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
| Stream Deck Mini | `0x0060` | 6 |
| Stream Deck XL | `0x006c` | 32 |
| Stream Deck MK.2 | `0x0080` | 15 |
| Stream Deck Original V2 | `0x0063` | 15 |

---

## License

MIT
