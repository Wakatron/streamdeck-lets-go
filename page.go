package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/disintegration/gift"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"streamdeck-lets-go/internal/config"
)

type keyState struct {
	cancel context.CancelFunc
}

type DisplayOutput struct {
	Text       string `json:"text"`
	Background string `json:"background,omitempty"`
	TextColor  string `json:"text_color,omitempty"`
}

type PageManager struct {
	mu             sync.RWMutex
	pages          map[string]*config.PageConfig
	active         string
	deck           *Deck
	keyStates      map[int]*keyState
	displayOutputs map[int]*DisplayOutput
	displayMu      sync.RWMutex
	defaultFont    string
}

func NewPageManager(deck *Deck) *PageManager {
	return &PageManager{
		pages:          make(map[string]*config.PageConfig),
		deck:           deck,
		keyStates:      make(map[int]*keyState),
		displayOutputs: make(map[int]*DisplayOutput),
		defaultFont:    "medium",
	}
}

func (pm *PageManager) LoadPages(pages []config.PageConfig) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pages = make(map[string]*config.PageConfig, len(pages))
	for i := range pages {
		pm.pages[pages[i].Name] = &pages[i]
	}
}

func (pm *PageManager) ActivatePage(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	page, ok := pm.pages[name]
	if !ok {
		return fmt.Errorf("page %q not found", name)
	}

	slog.Info("activating page", "name", name)
	pm.active = name

	pm.displayMu.Lock()
	pm.displayOutputs = make(map[int]*DisplayOutput)
	pm.displayMu.Unlock()

	if err := pm.deck.ClearAll(); err != nil {
		slog.Warn("clear all failed", "error", err)
	}

	if page.Background != "" {
		if img, err := loadImage(page.Background, 0, 0); err == nil {
			if err := pm.deck.FillPanel(img); err != nil {
				slog.Warn("fill panel failed", "error", err)
			}
		} else {
			slog.Warn("load background image", "path", page.Background, "error", err)
		}
	}

	keyByIndex := make(map[int]config.KeyConfig, len(page.Keys))
	for _, k := range page.Keys {
		keyByIndex[k.Index] = k
	}

	for i := 0; i < pm.deck.NumKeys(); i++ {
		k, ok := keyByIndex[i]
		if !ok {
			if page.Background == "" {
				pm.deck.ClearKey(i)
			}
			continue
		}
		pm.renderKey(i, &k)
	}

	return nil
}

func (pm *PageManager) ActivePageName() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.active
}

func (pm *PageManager) ActivePage() *config.PageConfig {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.pages[pm.active]
}

func (pm *PageManager) GetDisplayOutputs() map[int]*DisplayOutput {
	pm.displayMu.RLock()
	defer pm.displayMu.RUnlock()
	result := make(map[int]*DisplayOutput, len(pm.displayOutputs))
	for k, v := range pm.displayOutputs {
		result[k] = v
	}
	return result
}

func (pm *PageManager) GetPage(name string) *config.PageConfig {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.pages[name]
}

func (pm *PageManager) RenderKey(keyIndex int) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	page := pm.pages[pm.active]
	if page == nil {
		return
	}
	for _, k := range page.Keys {
		if k.Index == keyIndex {
			pm.renderKey(keyIndex, &k)
			return
		}
	}
}

func (pm *PageManager) ReRenderDisplayKey(keyIndex int, output string) {
	pm.mu.RLock()
	page := pm.pages[pm.active]
	pm.mu.RUnlock()
	if page == nil {
		return
	}
	for _, k := range page.Keys {
		if k.Index == keyIndex && k.Display != nil {
			pm.renderKeyOutput(keyIndex, k.Display, output, nil)
			return
		}
	}
}

func (pm *PageManager) renderKey(idx int, k *config.KeyConfig) {
	fontName := k.Font
	if fontName == "" {
		fontName = pm.defaultFont
	}
	fontSize := 18.0
	if k.FontSize != nil {
		fontSize = *k.FontSize
	}

	faScale := 0.55
	if k.IconScale != nil {
		faScale = *k.IconScale
	}

	if k.Icon != "" {
		img, err := loadImage(k.Icon, pm.deck.KeySize(), faScale)
		if err != nil {
			slog.Warn("load icon", "path", k.Icon, "error", err)
			pm.deck.FillColor(idx, 64, 64, 64)
			if k.Label != "" {
				pm.deck.WriteText(idx, k.Label, image.Black, fontName, fontSize)
			}
			return
		}

		if k.Background != "" {
			if bg, err := parseHexColor(k.Background); err == nil {
				img = applyBackground(img, bg)
			} else {
				slog.Warn("invalid background color", "value", k.Background, "error", err)
			}
		}

		onImgFontSize := fontSize
		if k.FontSize == nil {
			onImgFontSize = 16
		}
		onImgFontName := fontName
		if k.Font == "" {
			onImgFontName = pm.defaultFont
		}

		if k.Label != "" {
			if err := pm.deck.WriteTextOnImage(idx, img, k.Label, onImgFontName, onImgFontSize); err != nil {
				slog.Warn("write text on image", "error", err)
				pm.deck.FillImage(idx, img)
			}
		} else {
			if err := pm.deck.FillImage(idx, img); err != nil {
				slog.Warn("fill image", "error", err)
			}
		}
	} else if k.Label != "" {
		if k.Background != "" {
			bg, err := parseHexColor(k.Background)
			if err == nil {
				ks := pm.deck.KeySize()
				img := image.NewRGBA(image.Rect(0, 0, ks, ks))
				draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
				if err := pm.deck.WriteTextOnImage(idx, img, k.Label, fontName, fontSize); err != nil {
					slog.Warn("write text on image", "error", err)
					pm.deck.FillImage(idx, img)
				}
				return
			}
			slog.Warn("invalid background color", "value", k.Background, "error", err)
		}
		if err := pm.deck.WriteText(idx, k.Label, image.Black, fontName, fontSize); err != nil {
			slog.Warn("write text", "error", err)
		}
	}
}

func (pm *PageManager) stopPeriodicKeys() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, ks := range pm.keyStates {
		ks.cancel()
	}
	pm.keyStates = make(map[int]*keyState)
}

func (pm *PageManager) startPeriodicKeys() {
	pm.mu.Lock()
	page := pm.pages[pm.active]
	if page == nil {
		pm.mu.Unlock()
		return
	}

	type displayKey struct {
		index   int
		display *config.DisplayCfg
		ctx     context.Context
	}
	var keys []displayKey
	for _, k := range page.Keys {
		if k.Display != nil && k.Display.Interval != "" {
			interval, err := time.ParseDuration(k.Display.Interval)
			if err != nil || interval < time.Second {
				continue
			}
			if _, exists := pm.keyStates[k.Index]; exists {
				continue
			}
			ctx, cancel := context.WithCancel(context.Background())
			pm.keyStates[k.Index] = &keyState{cancel: cancel}
			keys = append(keys, displayKey{index: k.Index, display: k.Display, ctx: ctx})
		}
	}
	pm.mu.Unlock()

	for _, dk := range keys {
		go pm.runDisplayKey(dk.index, dk.display, dk.ctx)
	}
}

func (pm *PageManager) runDisplayKey(idx int, d *config.DisplayCfg, ctx context.Context) {
	timeout := 30 * time.Second
	if d.Timeout != "" {
		if t, err := time.ParseDuration(d.Timeout); err == nil && t > 0 {
			timeout = t
		}
	}
	interval, _ := time.ParseDuration(d.Interval)

	output, execErr := execDisplayCapture(d, timeout)
	pm.renderKeyOutput(idx, d, output, execErr)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			output, execErr := execDisplayCapture(d, timeout)
			pm.renderKeyOutput(idx, d, output, execErr)
		case <-ctx.Done():
			return
		}
	}
}

type displayFormat struct {
	Text       string `json:"text"`
	Background string `json:"background,omitempty"`
	TextColor  string `json:"text_color,omitempty"`
}

func looksLikeHex(s string) bool {
	if len(s) != 7 && len(s) != 9 {
		return false
	}
	if s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func parseDisplayOutput(output string) (*DisplayOutput, color.Color, color.Color) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return &DisplayOutput{}, nil, nil
	}

	var do DisplayOutput
	var bgColor, fgColor color.Color
	if err := json.Unmarshal([]byte(trimmed), &do); err == nil && (do.Text != "" || do.Background != "") {
		if do.Background != "" {
			if c, err := parseHexColor(do.Background); err == nil {
				bgColor = c
			}
		}
		if do.TextColor != "" {
			if c, err := parseHexColor(do.TextColor); err == nil {
				fgColor = c
			}
		}
		do.Text = sanitizeText(do.Text)
		return &do, bgColor, fgColor
	}

	lines := strings.SplitN(trimmed, "\n", 2)
	if len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		if looksLikeHex(first) {
			do.Background = first
			if c, err := parseHexColor(first); err == nil {
				bgColor = c
			}
			if len(lines) > 1 {
				do.Text = sanitizeText(lines[1])
			}
			return &do, bgColor, fgColor
		}
	}

	do.Text = sanitizeText(trimmed)
	return &do, bgColor, fgColor
}

func (pm *PageManager) renderKeyOutput(idx int, d *config.DisplayCfg, output string, execErr error) {
	pm.mu.RLock()
	page := pm.pages[pm.active]
	pm.mu.RUnlock()
	if page == nil {
		return
	}

	var kc *config.KeyConfig
	for _, k := range page.Keys {
		if k.Index == idx {
			kCopy := k
			kc = &kCopy
			break
		}
	}
	if kc == nil {
		return
	}

	if execErr != nil {
		slog.Warn("display command failed", "key", idx, "error", execErr)
	}

	do, bgOverride, fgOverride := parseDisplayOutput(output)
	fg := fgOverride
	if fg == nil {
		fg = color.White
	}

	if do.Text == "" && bgOverride == nil {
		do = &DisplayOutput{}
	}

	pm.displayMu.Lock()
	pm.displayOutputs[idx] = do
	pm.displayMu.Unlock()

	maxLen := d.MaxLen
	if maxLen <= 0 {
		maxLen = 128
	}
	if len(do.Text) > maxLen {
		do.Text = do.Text[:maxLen]
	}

	text := do.Text

	faScale := 0.55
	if kc.IconScale != nil {
		faScale = *kc.IconScale
	}

	fontSize := 12.0
	if kc.FontSize != nil {
		fontSize = *kc.FontSize
	}

	if kc.Icon != "" {
		img, err := loadImage(kc.Icon, pm.deck.KeySize(), faScale)
		if err != nil {
			slog.Warn("load icon for display", "path", kc.Icon, "error", err)
			var bg color.Color = color.Black
			if bgOverride != nil {
				bg = bgOverride
			}
			textImg := renderUnicodeText(text, fontSize, pm.deck.KeySize(), bg, fg)
			if err := pm.deck.FillImage(idx, textImg); err != nil {
				slog.Warn("fill image", "error", err)
			}
			return
		}

		if kc.Background != "" && bgOverride == nil {
			if bg, err := parseHexColor(kc.Background); err == nil {
				img = applyBackground(img, bg)
			} else {
				slog.Warn("invalid background color", "value", kc.Background, "error", err)
			}
		} else if bgOverride != nil {
			if rgba, ok := bgOverride.(color.RGBA); ok {
				img = applyBackground(img, rgba)
			}
		}

		composite := renderUnicodeTextOnImage(img, text, fontSize, pm.deck.KeySize(), fg)
		if err := pm.deck.FillImage(idx, composite); err != nil {
			slog.Warn("fill image", "error", err)
		}
		return
	}

	var bg color.Color = color.Black
	if bgOverride != nil {
		bg = bgOverride
	} else if kc.Background != "" {
		if c, err := parseHexColor(kc.Background); err == nil {
			bg = c
		}
	}
	textImg := renderUnicodeText(text, fontSize, pm.deck.KeySize(), bg, fg)
	if err := pm.deck.FillImage(idx, textImg); err != nil {
		slog.Warn("fill image", "error", err)
	}
}

var (
	unicodeFontOnce sync.Once
	unicodeFontData []byte
)

func loadUnicodeFontBytes() []byte {
	unicodeFontOnce.Do(func() {
		if data := fcRead(":charset=20BD"); data != nil {
			unicodeFontData = data
			return
		}
		if data := fcRead("mono"); data != nil {
			unicodeFontData = data
			return
		}
		if data := fcRead("sans"); data != nil {
			unicodeFontData = data
			return
		}
		unicodeFontData = goregular.TTF
	})
	return unicodeFontData
}

var (
	emojiFontOnce sync.Once
	emojiFontData []byte
)

func loadEmojiFontBytes() []byte {
	emojiFontOnce.Do(func() {
		if data := fcRead("emoji"); data != nil {
			if _, err := opentype.Parse(data); err == nil {
				emojiFontData = data
				return
			}
		}
		emojiFontData = loadUnicodeFontBytes()
	})
	return emojiFontData
}

func renderEmojiGlyph(r rune, size int, scale float64) image.Image {
	if img, ok := renderCBDTGlyph(r, size, scale); ok {
		return img
	}

	fontSize := float64(size) * scale
	data := loadEmojiFontBytes()
	fnt, err := opentype.Parse(data)
	if err != nil {
		return renderUnicodeText(string(r), fontSize, size, color.Black, color.White)
	}
	face, err := opentype.NewFace(fnt, &opentype.FaceOptions{
		Size:    fontSize,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return renderUnicodeText(string(r), fontSize, size, color.Black, color.White)
	}
	defer face.Close()

	rgba := image.NewRGBA(image.Rect(0, 0, size, size))

	adv := font.MeasureString(face, string(r)).Ceil()
	metrics := face.Metrics()
	height := metrics.Height.Ceil()

	x := (size - adv) / 2
	if x < 1 {
		x = 1
	}
	y := (size + height) / 2
	if y < 1 {
		y = 1
	}

	d := &font.Drawer{
		Dst:  rgba,
		Src:  image.NewUniform(color.White),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(string(r))
	return rgba
}

func fcRead(pattern string) []byte {
	cmd := exec.Command("fc-match", "-f", "%{file}", pattern)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

func parseDisplayFace(size float64) (font.Face, error) {
	f, err := opentype.Parse(loadUnicodeFontBytes())
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

func renderUnicodeText(text string, fontSize float64, keySize int, bg, fg color.Color) *image.RGBA {
	rgba := image.NewRGBA(image.Rect(0, 0, keySize, keySize))
	draw.Draw(rgba, rgba.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	if text == "" || fg == nil {
		return rgba
	}

	face, err := parseDisplayFace(fontSize)
	if err != nil {
		return rgba
	}
	defer face.Close()

	textAdvance := font.MeasureString(face, text).Ceil()
	metrics := face.Metrics()
	height := metrics.Height.Ceil()

	x := (keySize - textAdvance) / 2
	if x < 1 {
		x = 1
	}
	y := (keySize + height) / 2
	if y < 1 {
		y = 1
	}

	d := &font.Drawer{
		Dst:  rgba,
		Src:  &image.Uniform{fg},
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)

	return rgba
}

func renderUnicodeTextOnImage(baseImg image.Image, text string, fontSize float64, keySize int, fg color.Color) *image.RGBA {
	g := gift.New(gift.Resize(keySize, keySize, gift.LanczosResampling))
	rgba := image.NewRGBA(image.Rect(0, 0, keySize, keySize))
	g.Draw(rgba, baseImg)

	barHeight := 20
	if keySize < 72 {
		barHeight = 16
	}
	barRect := image.Rect(0, keySize-barHeight, keySize, keySize)
	draw.Draw(rgba, barRect, &image.Uniform{color.RGBA{0, 0, 0, 180}}, image.Point{}, draw.Over)

	if text == "" || fg == nil {
		return rgba
	}

	face, err := parseDisplayFace(fontSize)
	if err != nil {
		return rgba
	}
	defer face.Close()

	textAdvance := font.MeasureString(face, text).Ceil()
	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()

	x := (keySize - textAdvance) / 2
	if x < 1 {
		x = 1
	}
	y := keySize - barHeight/2 + ascent/2
	if y > keySize-2 {
		y = keySize - 2
	}

	d := &font.Drawer{
		Dst:  rgba,
		Src:  &image.Uniform{fg},
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)

	return rgba
}

func sanitizeText(s string) string {
	text := strings.TrimSpace(s)
	if text == "" {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	inEscape := false
	for _, r := range text {
		if inEscape {
			if unicode.IsLetter(r) {
				inEscape = false
			}
			continue
		}
		if r == 0x1b {
			inEscape = true
			continue
		}
		if unicode.IsPrint(r) || r == '\n' {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func parseHexColor(s string) (color.RGBA, error) {
	if len(s) == 0 || s[0] != '#' {
		return color.RGBA{}, fmt.Errorf("invalid color format: %q", s)
	}
	raw := strings.TrimPrefix(s, "#")
	if len(raw) != 3 && len(raw) != 6 && len(raw) != 8 {
		return color.RGBA{}, fmt.Errorf("invalid color length: %q", s)
	}
	if len(raw) == 3 {
		raw = string([]byte{raw[0], raw[0], raw[1], raw[1], raw[2], raw[2]})
	}
	n, err := strconv.ParseUint(raw, 16, 64)
	if err != nil {
		return color.RGBA{}, fmt.Errorf("invalid hex color: %q: %w", s, err)
	}
	switch len(raw) {
	case 6:
		return color.RGBA{
			R: uint8(n >> 16),
			G: uint8(n >> 8),
			B: uint8(n),
			A: 255,
		}, nil
	case 8:
		return color.RGBA{
			R: uint8(n >> 24),
			G: uint8(n >> 16),
			B: uint8(n >> 8),
			A: uint8(n),
		}, nil
	}
	return color.RGBA{}, fmt.Errorf("unexpected hex length: %d", len(raw))
}

func applyBackground(img image.Image, bg color.RGBA) *image.RGBA {
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, &image.Uniform{bg}, image.Point{}, draw.Src)
	draw.Draw(rgba, b, img, image.Point{}, draw.Over)
	return rgba
}

var imageCache sync.Map

func isEmojiRef(path string) bool {
	return strings.HasPrefix(path, "emoji:")
}

func parseEmojiRef(path string) (rune, error) {
	raw := strings.TrimPrefix(path, "emoji:")
	if raw == "" {
		return 0, fmt.Errorf("empty emoji ref")
	}

	if strings.HasPrefix(raw, ":") && strings.HasSuffix(raw, ":") && len(raw) > 2 {
		name := strings.ToLower(raw[1 : len(raw)-1])
		if r, ok := emojiShortcodes[name]; ok {
			return r, nil
		}
		return 0, fmt.Errorf("unknown emoji shortcode: %s", name)
	}

	r, size := utf8.DecodeRuneInString(raw)
	if r == utf8.RuneError || size == 0 {
		return 0, fmt.Errorf("invalid emoji character")
	}
	return r, nil
}

func cacheKey(path string, targetSize int, faScale float64) string {
	return fmt.Sprintf("%s|%d|%.6f", path, targetSize, faScale)
}

func loadImage(path string, targetSize int, faScale float64) (image.Image, error) {
	key := cacheKey(path, targetSize, faScale)
	if cached, ok := imageCache.Load(key); ok {
		return cached.(image.Image), nil
	}

	if isFAIconRef(path) {
		style, name, err := parseFAIcon(path)
		if err != nil {
			return nil, err
		}
		img, err := renderFAGlyph(style, name, targetSize, faScale)
		if err != nil {
			return nil, err
		}
		imageCache.Store(key, img)
		return img, nil
	}

	if isEmojiRef(path) {
		r, err := parseEmojiRef(path)
		if err != nil {
			return nil, err
		}
		img := renderEmojiGlyph(r, targetSize, faScale)
		imageCache.Store(key, img)
		return img, nil
	}

	resolved := path
	if isSystemIconRef(path) {
		p, err := findSystemIcon(systemIconName(path), targetSize)
		if err != nil {
			return nil, err
		}
		resolved = p
	} else if !strings.HasPrefix(path, "/") {
		resolved = filepath.Join(configDir(), path)
	}

	if strings.HasSuffix(resolved, ".svg") {
		img, err := svgToPNG(resolved, targetSize)
		if err != nil {
			return nil, err
		}
		imageCache.Store(key, img)
		return img, nil
	}

	f, err := os.Open(resolved)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	imageCache.Store(key, img)
	return img, nil
}

var cachedConfigDir string

func configDir() string {
	if cachedConfigDir != "" {
		return cachedConfigDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		cachedConfigDir = "."
		return cachedConfigDir
	}
	cachedConfigDir = filepath.Join(home, ".config", "streamdeck-lets-go")
	return cachedConfigDir
}
