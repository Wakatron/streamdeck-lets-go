package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type Config struct {
	Version     int            `json:"version"`
	LogLevel    string         `json:"log_level"`
	DefaultPage string         `json:"default_page"`
	Devices     []DeviceConfig `json:"devices"`
	Pages       []PageConfig   `json:"pages"`
	AutoSwitch  []SwitchRule   `json:"auto_switch"`
	Screensaver ScreensaverCfg `json:"screensaver"`
}

type DeviceConfig struct {
	Serial     string `json:"serial"`
	Brightness int    `json:"brightness"`
	Rotation   int    `json:"rotation,omitempty"`
}

type PageConfig struct {
	Name        string          `json:"name"`
	Icon        string          `json:"icon,omitempty"`
	Keys        []KeyConfig     `json:"keys"`
	Background  string          `json:"background,omitempty"`
	Screensaver *ScreensaverCfg `json:"screensaver,omitempty"`
}

type KeyConfig struct {
	Index      int         `json:"index"`
	Icon       string      `json:"icon,omitempty"`
	Background string      `json:"background,omitempty"`
	Label      string      `json:"label,omitempty"`
	Font       string      `json:"font,omitempty"`
	FontSize   *float64    `json:"font_size,omitempty"`
	IconScale  *float64    `json:"icon_scale,omitempty"`
	Action     *Action     `json:"action,omitempty"`
	Display    *DisplayCfg `json:"display,omitempty"`
}

type Action struct {
	Type       string `json:"type"`
	Command    string `json:"command,omitempty"`
	Builtin    string `json:"builtin,omitempty"`
	Script     string `json:"script,omitempty"`
	Page       string `json:"page,omitempty"`
	Background bool   `json:"background,omitempty"`
	Keys       string `json:"keys,omitempty"`
}

type DisplayCfg struct {
	Command  string `json:"command,omitempty"`
	Script   string `json:"script,omitempty"`
	Interval string `json:"interval"`
	MaxLen   int    `json:"max_len,omitempty"`
	Timeout  string `json:"timeout,omitempty"`
}

type SwitchRule struct {
	WMClass string   `json:"wm_class"`
	Title   string   `json:"title,omitempty"`
	Page    string   `json:"page"`
	Devices []string `json:"devices,omitempty"`
	Stay    bool     `json:"stay,omitempty"`
}

type ScreensaverCfg struct {
	Enabled     bool   `json:"enabled"`
	IdleSeconds int    `json:"idle_seconds,omitempty"`
	Image       string `json:"image,omitempty"`
	Brightness  int    `json:"brightness,omitempty"`
}

func DefaultConfig() *Config {
	return &Config{
		Version:     1,
		LogLevel:    "info",
		DefaultPage: "default",
		Pages: []PageConfig{
			{
				Name: "default",
				Keys: []KeyConfig{},
			},
		},
	}
}

func ConfigPath(path string) string {
	if path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(home, ".config", "streamdeck-lets-go", "config.json")
}

func LoadConfig(path string) (*Config, error) {
	path = ConfigPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("config not found, using defaults", "path", path)
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) Save(path string) error {
	path = ConfigPath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

func (c *Config) Validate() error {
	if c.Version < 1 {
		return fmt.Errorf("unsupported config version: %d", c.Version)
	}
	if c.DefaultPage == "" {
		return fmt.Errorf("default_page is required")
	}
	pageNames := make(map[string]bool)
	for _, p := range c.Pages {
		if p.Name == "" {
			return fmt.Errorf("page name is required")
		}
		if pageNames[p.Name] {
			return fmt.Errorf("duplicate page name: %s", p.Name)
		}
		pageNames[p.Name] = true

		seenKeys := make(map[int]bool)
		maxKey := 31
		for _, k := range p.Keys {
			if k.Index < 0 {
				return fmt.Errorf("page %s: key index must be >= 0", p.Name)
			}
			if k.Index > maxKey {
				return fmt.Errorf("page %s: key index %d exceeds max %d", p.Name, k.Index, maxKey)
			}
			if seenKeys[k.Index] {
				return fmt.Errorf("page %s: duplicate key index %d", p.Name, k.Index)
			}
			seenKeys[k.Index] = true

			if k.Action != nil {
				switch k.Action.Type {
				case "command":
					if k.Action.Command == "" {
						return fmt.Errorf("page %s key %d: command required for command action", p.Name, k.Index)
					}
				case "builtin":
				case "script":
					if k.Action.Script == "" {
						return fmt.Errorf("page %s key %d: script path required for script action", p.Name, k.Index)
					}
				case "page":
					if k.Action.Page == "" {
						return fmt.Errorf("page %s key %d: page name required for page action", p.Name, k.Index)
					}
				case "keyboard":
					if k.Action.Keys == "" {
						return fmt.Errorf("page %s key %d: keys required for keyboard action", p.Name, k.Index)
					}
				case "":
					return fmt.Errorf("page %s key %d: action type is required", p.Name, k.Index)
				default:
					return fmt.Errorf("page %s key %d: unknown action type: %s", p.Name, k.Index, k.Action.Type)
				}
			}

			if k.Display != nil {
				if k.Display.Command == "" && k.Display.Script == "" {
					return fmt.Errorf("page %s key %d: display requires command or script", p.Name, k.Index)
				}
				if k.Display.Command != "" && k.Display.Script != "" {
					return fmt.Errorf("page %s key %d: display command and script are mutually exclusive", p.Name, k.Index)
				}
				if k.Display.Interval == "" {
					return fmt.Errorf("page %s key %d: display interval is required", p.Name, k.Index)
				}
				interval, err := time.ParseDuration(k.Display.Interval)
				if err != nil {
					return fmt.Errorf("page %s key %d: invalid display interval %q: %w", p.Name, k.Index, k.Display.Interval, err)
				}
				if interval < time.Second {
					return fmt.Errorf("page %s key %d: display interval must be at least 1s", p.Name, k.Index)
				}
				if interval > 24*time.Hour {
					return fmt.Errorf("page %s key %d: display interval must not exceed 24h", p.Name, k.Index)
				}
				if k.Display.Timeout != "" {
					if _, err := time.ParseDuration(k.Display.Timeout); err != nil {
						return fmt.Errorf("page %s key %d: invalid display timeout %q: %w", p.Name, k.Index, k.Display.Timeout, err)
					}
				}
				if k.Display.MaxLen < 0 {
					return fmt.Errorf("page %s key %d: max_len must be >= 0", p.Name, k.Index)
				}
				if k.Display.MaxLen > 4096 {
					return fmt.Errorf("page %s key %d: max_len must be <= 4096", p.Name, k.Index)
				}
			}
		}
	}

	for _, p := range c.Pages {
		for _, k := range p.Keys {
			if k.Action != nil && k.Action.Type == "page" {
				if !pageNames[k.Action.Page] {
					return fmt.Errorf("page %s key %d: references unknown page %q", p.Name, k.Index, k.Action.Page)
				}
			}
		}
	}

	for _, sr := range c.AutoSwitch {
		if sr.WMClass == "" && sr.Title == "" {
			return fmt.Errorf("auto_switch rule: wm_class or title is required")
		}
		if !pageNames[sr.Page] {
			return fmt.Errorf("auto_switch rule: references unknown page %q", sr.Page)
		}
		if _, err := regexp.Compile(sr.WMClass); sr.WMClass != "" && err != nil {
			return fmt.Errorf("auto_switch rule: invalid wm_class regex %q: %w", sr.WMClass, err)
		}
		if _, err := regexp.Compile(sr.Title); sr.Title != "" && err != nil {
			return fmt.Errorf("auto_switch rule: invalid title regex %q: %w", sr.Title, err)
		}
	}

	if !pageNames[c.DefaultPage] {
		return fmt.Errorf("default_page %q not found in pages", c.DefaultPage)
	}
	return nil
}
