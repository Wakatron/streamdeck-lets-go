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

### Dynamic Key Generators

Generate an entire page of keys dynamically by running any executable (shell script, Python, Go binary, etc.). Useful for game launchers, music collections, Docker containers, monitoring dashboards — anything where the set of keys is not known at config time.

```
┌──────────────────────────────────────────────────┐
│  Generator script (any language)                 │
│  → queries Lutris DB, Steam API, etc.           │
│  → outputs JSON array of KeyConfig to stdout     │
│  → receives STREAMDECK_KEY_COUNT env var         │
└──────────────┬───────────────────────────────────┘
               │ stdout: [{"index":0,"icon":"...",...}]
               ▼
┌──────────────────────────────────────────────────┐
│  streamdeck-lets-go daemon                       │
│  → parses JSON                                   │
│  → merges with static keys (static wins)         │
│  → renders keys on the deck                      │
│  → re-runs on interval (default 60s)             │
└──────────────────────────────────────────────────┘
```

#### Config

Add `dynamic_keys` to any page:

```json
{
  "name": "Games",
  "icon": "fa:gamepad",
  "keys": [
    {
      "index": 0,
      "icon": "fa:arrow-left",
      "actions": [
        { "trigger": "tap", "type": "page", "page": "main" }
      ]
    }
  ],
  "dynamic_keys": {
    "command": "/home/user/bin/game-list",
    "interval": "120s",
    "timeout": "15s",
    "max_keys": 14
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `command` | string | — | Shell command to execute (mutually exclusive with `script`) |
| `script` | string | — | Path to an executable script; relative paths resolve against `~/.config/streamdeck-lets-go/` |
| `interval` | string | `"60s"` | How often to re-run the generator (minimum 1s, maximum 24h) |
| `timeout` | string | `"15s"` | Maximum execution time before the generator is killed |
| `max_keys` | int | `deck.NumKeys()` | Maximum number of keys to accept (e.g. `13` to reserve 2 for navigation) |

#### Generator contract

The script **must** print a JSON array of key configs to stdout and exit with code 0. Each element supports the full `KeyConfig` schema:

```json
[
  {
    "index": 0,
    "icon": "/home/user/.local/share/lutris/coverart/cyberpunk-2077.jpg",
    "label": "Cyberpunk 2077",
    "font_size": 12,
    "icon_scale": 1.0,
    "background": "#222222",
    "actions": [
      {
        "trigger": "tap",
        "type": "command",
        "command": "lutris lutris:rungame/cyberpunk-2077",
        "background": true
      }
    ]
  }
]
```

Every field from a static key is available:

| Field | Type | Description |
|---|---|---|
| `index` | int | Key position on the deck (0-based) |
| `icon` | string | Icon source: `fa:terminal`, `emoji:fire`, `@firefox`, `/absolute/path.png`, relative path |
| `label` | string | Text shown below the icon |
| `font_size` | number | Font size in points (default 18, recommended 11–14 for labels) |
| `icon_scale` | number | Icon scale factor (0.0–1.0, default 0.55) |
| `background` | string | Hex background color `"#222222"` |
| `actions` | array | Same action schema as static keys — supports all triggers (`tap`, `long_press`, `double_tap`) and all action types (`command`, `builtin`, `script`, `page`, `keyboard`) |
| `display` | object | Periodic display block (same as static key `display`) — command/script runs on interval and text is overlaid on the key |

#### Merge behavior

Static `keys` (defined directly on the page) and dynamic keys are **merged** at runtime:

| Priority | Source |
|---|---|
| 1 (highest) | Static keys — their indices are reserved |
| 2 | Dynamic keys — fill indices not occupied by static keys |

This lets you keep navigation buttons fixed while the generator fills the rest of the deck:

```
Static:   [← back] [  ] [  ] [  ] [  ] [  ] [  ] [  ] [  ] [  ] [  ] [  ] [  ] [  ] [  ]
            index 0   └────────────────────── dynamic (indices 1–14) ──────────────────────→
```

#### Environment variables

The generator receives these environment variables:

| Variable | Example | Description |
|---|---|---|
| `STREAMDECK_KEY_COUNT` | `15` | Maximum number of keys to output (derived from deck model or `max_keys` config) |
| `STREAMDECK_CONFIG_DIR` | `/home/user/.config/streamdeck-lets-go` | Path to the config directory — useful for finding companion files |

#### Error handling

| Situation | Behavior |
|---|---|
| Script fails (non-zero exit, timeout) | Warning logged; previous keys preserved |
| Script returns empty array `[]` | Only static keys rendered |
| Script returns invalid JSON | Warning logged; previous keys preserved |
| Script outputs debug to stderr | Logged at DEBUG level; stdout JSON parsed cleanly |
| More keys than `max_keys` | Excess keys silently dropped |
| Generator updates while page is active | Keys re-rendered in place (no flicker) |

#### Examples

**Python — Lutris game launcher**

Place this at `~/.config/streamdeck-lets-go/scripts/lutris-keys`, make executable (`chmod +x`).

```python
#!/usr/bin/env python3
import json, os, sqlite3, pathlib

db = pathlib.Path.home() / '.local/share/lutris/pga.db'
cover = pathlib.Path.home() / '.local/share/lutris/coverart'
max_keys = int(os.environ.get('STREAMDECK_KEY_COUNT', 15))

if not db.exists():
    print('[]')
    exit(0)

conn = sqlite3.connect(db)
games = conn.execute(
    "SELECT slug, name FROM games WHERE installed = 1 ORDER BY name"
).fetchall()
conn.close()

keys = []
for idx, (slug, name) in enumerate(games):
    if idx >= max_keys:
        break
    icon = next(cover.glob(f'{slug}.*'), None)
    key = {
        "index": idx,
        "icon_scale": 1.0,
        "actions": [{
            "trigger": "tap",
            "type": "command",
            "command": f"lutris lutris:rungame/{slug}",
            "background": True
        }]
    }
    if icon:
        key["icon"] = str(icon)
    keys.append(key)

print(json.dumps(keys))
```

Reference it in config:
```json
{
  "name": "Games",
  "dynamic_keys": {
    "script": "scripts/lutris-keys",
    "interval": "120s"
  }
}
```

**Shell — Simple test generator**

```bash
#!/bin/sh
for i in $(seq 0 $((STREAMDECK_KEY_COUNT - 1))); do
  [ "$i" -gt 0 ] && echo ","
  printf '{"index":%d,"icon":"fa:hashtag","label":"Item %d"}' "$i" "$i"
done
```

Run with any language you like — the only requirement is a valid JSON array on stdout.

#### Tips

- Use `max_keys` to reserve space for static navigation buttons
- Enable `show_label_background: true` in the global config if labels on full-bleed icons are hard to read
- Test your generator standalone before wiring it up: `STREAMDECK_KEY_COUNT=15 ./my-generator`
- For large data sources, pass pagination params through the command string: `"command": "scripts/albums --page 0"`
- Dynamic keys support `display` blocks — nest monitoring data inside generated keys

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

## Credits

This project builds on the work of the following open-source libraries:

- **[dh1tw/streamdeck](https://github.com/dh1tw/streamdeck)** (MIT) — Go protocol driver for Elgato Stream Deck devices
- **[bearsh/hid](https://github.com/bearsh/hid)** (MIT) — Go USB HID bindings via CGO / libusb
- **[disintegration/gift](https://github.com/disintegration/gift)** (MIT) — Go Image Filtering Toolkit

---

## License

MIT
