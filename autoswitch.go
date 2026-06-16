package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"streamdeck-lets-go/internal/config"
)

type WindowDetector interface {
	Start(ctx context.Context) (<-chan Window, error)
	Close()
}

type Window struct {
	WMClass string
	Title   string
}

func NewWindowDetector() WindowDetector {
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	wayland := os.Getenv("WAYLAND_DISPLAY")

	slog.Debug("detecting desktop environment", "XDG_CURRENT_DESKTOP", desktop, "WAYLAND_DISPLAY", wayland)

	switch {
	case desktop == "Hyprland":
		slog.Info("auto-switch: using Hyprland detector")
		d := &hyprlandDetector{}
		d.PollInterval = 200 * time.Millisecond
		return d

	case desktop == "sway":
		slog.Info("auto-switch: using Sway detector")
		d := &swayDetector{}
		d.PollInterval = 500 * time.Millisecond
		return d

	case desktop == "niri":
		slog.Info("auto-switch: using Niri detector")
		d := &niriDetector{}
		d.PollInterval = 200 * time.Millisecond
		return d

	case desktop == "GNOME" || strings.Contains(desktop, "GNOME"):
		slog.Info("auto-switch: GNOME detector (stub)")
		d := &gnomeDetector{}
		d.PollInterval = 500 * time.Millisecond
		return d

	case strings.Contains(desktop, "KDE") || strings.Contains(desktop, "plasma"):
		slog.Info("auto-switch: KDE detector (stub)")
		d := &kdeDetector{}
		d.PollInterval = 500 * time.Millisecond
		return d

	case wayland != "":
		slog.Warn("auto-switch: unknown Wayland compositor, using portal fallback")
		d := &portalDetector{}
		d.PollInterval = 500 * time.Millisecond
		return d

	default:
		slog.Info("auto-switch: using X11 detector")
		d := &x11Detector{}
		d.PollInterval = 200 * time.Millisecond
		return d
	}
}

type basePoll struct {
	PollInterval time.Duration
	pollFn       func(context.Context) Window
	cancel       context.CancelFunc
	lastRaw      string
}

func (b *basePoll) start(ctx context.Context, ch chan<- Window) {
	ticker := time.NewTicker(b.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			win := b.pollFn(ctx)
			raw := win.WMClass + "|" + win.Title
			if raw != b.lastRaw {
				b.lastRaw = raw
				select {
				case ch <- win:
				default:
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (b *basePoll) stop() {
	if b.cancel != nil {
		b.cancel()
	}
}

func runCmd(ctx context.Context, cmd string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, cmd, args...)
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func runCmdJSON(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, cmd, args...)
	return c.Output()
}

type hyprlandDetector struct {
	basePoll
}

func (d *hyprlandDetector) Start(ctx context.Context) (<-chan Window, error) {
	d.pollFn = d.getWindow
	ctx, d.cancel = context.WithCancel(ctx)
	ch := make(chan Window, 4)
	go d.basePoll.start(ctx, ch)
	return ch, nil
}

func (d *hyprlandDetector) Close() { d.basePoll.stop() }

type hyprlandWindow struct {
	Class string `json:"class"`
	Title string `json:"title"`
}

func (d *hyprlandDetector) getWindow(ctx context.Context) Window {
	data, err := runCmdJSON(ctx, "hyprctl", "activewindow", "-j")
	if err != nil {
		return Window{}
	}

	var win hyprlandWindow
	if err := json.Unmarshal(data, &win); err != nil {
		return Window{}
	}
	return Window{WMClass: win.Class, Title: win.Title}
}

type swayDetector struct {
	basePoll
}

func (d *swayDetector) Start(ctx context.Context) (<-chan Window, error) {
	d.pollFn = d.getWindow
	ctx, d.cancel = context.WithCancel(ctx)
	ch := make(chan Window, 4)
	go d.basePoll.start(ctx, ch)
	return ch, nil
}

func (d *swayDetector) Close() { d.basePoll.stop() }

type swayNode struct {
	Type     string           `json:"type"`
	Focused  bool             `json:"focused"`
	AppID    *string          `json:"app_id"`
	Name     string           `json:"name"`
	Nodes    []swayNode       `json:"nodes"`
	Floating []swayNode       `json:"floating_nodes"`
	Props    *swayWindowProps `json:"window_properties"`
}

type swayWindowProps struct {
	Class string `json:"class"`
}

func (d *swayDetector) getWindow(ctx context.Context) Window {
	data, err := runCmdJSON(ctx, "swaymsg", "-t", "get_tree")
	if err != nil {
		return Window{}
	}

	var root swayNode
	if err := json.Unmarshal(data, &root); err != nil {
		return Window{}
	}

	node := findFocusedSwayNode(&root)
	if node == nil {
		return Window{}
	}

	wmClass := ""
	if node.AppID != nil {
		wmClass = *node.AppID
	} else if node.Props != nil {
		wmClass = node.Props.Class
	}

	return Window{WMClass: wmClass, Title: node.Name}
}

func findFocusedSwayNode(node *swayNode) *swayNode {
	if node.Type == "con" && node.Focused {
		return node
	}
	for i := range node.Nodes {
		if found := findFocusedSwayNode(&node.Nodes[i]); found != nil {
			return found
		}
	}
	for i := range node.Floating {
		if found := findFocusedSwayNode(&node.Floating[i]); found != nil {
			return found
		}
	}
	return nil
}

type niriDetector struct {
	basePoll
}

func (d *niriDetector) Start(ctx context.Context) (<-chan Window, error) {
	d.pollFn = d.getWindow
	ctx, d.cancel = context.WithCancel(ctx)
	ch := make(chan Window, 4)
	go d.basePoll.start(ctx, ch)
	return ch, nil
}

func (d *niriDetector) Close() { d.basePoll.stop() }

type niriWindow struct {
	AppID *string `json:"app_id"`
	Title string  `json:"title"`
}

func (d *niriDetector) getWindow(ctx context.Context) Window {
	data, err := runCmdJSON(ctx, "niri", "msg", "--json", "focused-window")
	if err != nil {
		return Window{}
	}

	var win niriWindow
	if err := json.Unmarshal(data, &win); err != nil {
		return Window{}
	}
	if win.AppID == nil {
		return Window{}
	}

	return Window{WMClass: *win.AppID, Title: win.Title}
}

type gnomeDetector struct{ basePoll }
func (d *gnomeDetector) Start(ctx context.Context) (<-chan Window, error) {
	d.pollFn = d.getWindow
	ctx, d.cancel = context.WithCancel(ctx)
	ch := make(chan Window, 4)
	go d.basePoll.start(ctx, ch)
	return ch, nil
}
func (d *gnomeDetector) Close() { d.basePoll.stop() }

func (d *gnomeDetector) getWindow(ctx context.Context) Window {
	rawClass, err := runCmd(ctx, "gdbus", "call", "--session",
		"--dest", "org.gnome.Shell",
		"--object-path", "/org/gnome/Shell",
		"--method", "org.gnome.Shell.Eval",
		"global.display.focus_window?.get_wm_class() ?? ''")
	if err != nil {
		return Window{}
	}
	rawTitle, err := runCmd(ctx, "gdbus", "call", "--session",
		"--dest", "org.gnome.Shell",
		"--object-path", "/org/gnome/Shell",
		"--method", "org.gnome.Shell.Eval",
		"global.display.focus_window?.title ?? ''")
	if err != nil {
		_ = rawClass
		return Window{}
	}

	wmClass := parseGnomeEval(rawClass)
	title := parseGnomeEval(rawTitle)
	return Window{WMClass: wmClass, Title: title}
}

func parseGnomeEval(out string) string {
	out = strings.TrimSpace(out)
	out = strings.TrimPrefix(out, "(true, ")
	out = strings.TrimSuffix(out, ")")
	out = strings.Trim(out, `"'`)
	return out
}

type kdeDetector struct{ basePoll }
func (d *kdeDetector) Start(ctx context.Context) (<-chan Window, error) {
	d.pollFn = d.getWindow
	ctx, d.cancel = context.WithCancel(ctx)
	ch := make(chan Window, 4)
	go d.basePoll.start(ctx, ch)
	return ch, nil
}
func (d *kdeDetector) Close() { d.basePoll.stop() }

func (d *kdeDetector) getWindow(ctx context.Context) Window {
	wmClass, _ := runCmd(ctx, "kdotool", "getactivewindow", "getclassname")
	title, _ := runCmd(ctx, "kdotool", "getactivewindow", "getwindowname")
	return Window{WMClass: wmClass, Title: title}
}

type x11Detector struct{ basePoll }
func (d *x11Detector) Start(ctx context.Context) (<-chan Window, error) {
	d.pollFn = d.getWindow
	ctx, d.cancel = context.WithCancel(ctx)
	ch := make(chan Window, 4)
	go d.basePoll.start(ctx, ch)
	return ch, nil
}
func (d *x11Detector) Close() { d.basePoll.stop() }

func (d *x11Detector) getWindow(ctx context.Context) Window {
	wmClass, _ := runCmd(ctx, "xdotool", "getactivewindow", "getclassname")
	title, _ := runCmd(ctx, "xdotool", "getactivewindow", "getwindowname")
	return Window{WMClass: wmClass, Title: title}
}

type portalDetector struct{ basePoll }
func (d *portalDetector) Start(ctx context.Context) (<-chan Window, error) {
	d.pollFn = d.getWindow
	ctx, d.cancel = context.WithCancel(ctx)
	ch := make(chan Window, 4)
	go d.basePoll.start(ctx, ch)
	return ch, nil
}
func (d *portalDetector) Close() { d.basePoll.stop() }

func (d *portalDetector) getWindow(ctx context.Context) Window {
	return Window{}
}

type AutoSwitchManager struct {
	mu             sync.RWMutex
	rules          []compiledRule
	lastManualPage string
	autoPage       string
	autoStay       bool
}

type compiledRule struct {
	wmClass *regexp.Regexp
	title   *regexp.Regexp
	rule    config.SwitchRule
}

func NewAutoSwitchManager(rules []config.SwitchRule, defaultPage string) *AutoSwitchManager {
	m := &AutoSwitchManager{
		lastManualPage: defaultPage,
	}
	m.Reload(rules)
	return m
}

func (m *AutoSwitchManager) Reload(rules []config.SwitchRule) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.rules = make([]compiledRule, 0, len(rules))
	for _, r := range rules {
		cr := compiledRule{rule: r}
		if r.WMClass != "" {
			re, err := regexp.Compile(r.WMClass)
			if err != nil {
				slog.Warn("auto-switch: invalid wm_class regex", "pattern", r.WMClass, "error", err)
				continue
			}
			cr.wmClass = re
		}
		if r.Title != "" {
			re, err := regexp.Compile(r.Title)
			if err != nil {
				slog.Warn("auto-switch: invalid title regex", "pattern", r.Title, "error", err)
				continue
			}
			cr.title = re
		}
		m.rules = append(m.rules, cr)
	}

	slog.Info("auto-switch: rules loaded", "count", len(m.rules))
}

func (m *AutoSwitchManager) NotifyManualPage(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastManualPage = name
	m.autoPage = ""
	slog.Debug("auto-switch: manual page set", "page", name)
}

func (m *AutoSwitchManager) Evaluate(win Window, currentPage string) (page string, shouldSwitch bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if win.WMClass == "" && win.Title == "" {
		if m.autoPage != "" && m.autoPage == currentPage && !m.autoStay {
			m.autoPage = ""
			slog.Debug("auto-switch: no focused window, reverting to manual page", "page", m.lastManualPage)
			return m.lastManualPage, true
		}
		return "", false
	}

	for _, r := range m.rules {
		if r.wmClass == nil && r.title == nil {
			continue
		}
		if r.wmClass != nil && !r.wmClass.MatchString(win.WMClass) {
			continue
		}
		if r.title != nil && !r.title.MatchString(win.Title) {
			continue
		}

		m.autoPage = r.rule.Page
		m.autoStay = r.rule.Stay
		slog.Debug("auto-switch: rule matched",
			"wm_class", win.WMClass, "page", r.rule.Page, "stay", r.rule.Stay)
		return r.rule.Page, true
	}

	if m.autoPage != "" && m.autoPage == currentPage && !m.autoStay {
		m.autoPage = ""
		slog.Debug("auto-switch: reverting to manual page", "page", m.lastManualPage)
		return m.lastManualPage, true
	}

	return "", false
}
