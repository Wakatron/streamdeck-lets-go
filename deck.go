package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log/slog"
	"sync"
	"time"

	"github.com/bearsh/hid"
	"github.com/dh1tw/streamdeck"
	"github.com/disintegration/gift"
)

type DeviceInfo struct {
	Serial string
	Model  string
	PID    uint16
}

type DeckConfig struct {
	PID       uint16
	Name      string
	KeysX     int
	KeysY     int
	KeySize   int
	ImageFmt  string
	Rotate    bool
	Convert   bool
	HasDials  bool
	HasTouch  bool
}

var knownDecks = []DeckConfig{
	{PID: 0x60, Name: "Mini",      KeysX: 3, KeysY: 2, KeySize: 80, ImageFmt: "bmp",  Convert: true},
	{PID: 0x6d, Name: "Original",  KeysX: 5, KeysY: 3, KeySize: 72, ImageFmt: "jpg",  Rotate: true},
	{PID: 0x63, Name: "OriginalV2",KeysX: 5, KeysY: 3, KeySize: 72, ImageFmt: "bmp"},
	{PID: 0x80, Name: "MK2",       KeysX: 5, KeysY: 3, KeySize: 72, ImageFmt: "jpg",  Rotate: true},
	{PID: 0x6c, Name: "XL",        KeysX: 8, KeysY: 4, KeySize: 96, ImageFmt: "jpg"},
}

func findConfig(pid uint16) (DeckConfig, bool) {
	for _, d := range knownDecks {
		if d.PID == pid {
			return d, true
		}
	}
	return DeckConfig{}, false
}

func EnumerateDevices() ([]DeviceInfo, error) {
	devices := hid.Enumerate(streamdeck.VendorID, 0)
	if len(devices) == 0 {
		return nil, nil
	}
	var infos []DeviceInfo
	for _, d := range devices {
		info := DeviceInfo{Serial: d.Serial, PID: d.ProductID}
		if cfg, ok := findConfig(d.ProductID); ok {
			info.Model = cfg.Name
		} else {
			info.Model = fmt.Sprintf("Unknown (0x%04x)", d.ProductID)
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func toStreamDeckConfig(dc DeckConfig) *streamdeck.Config {
	return &streamdeck.Config{
		ProductID:        dc.PID,
		NumButtonColumns: dc.KeysX,
		NumButtonRows:    dc.KeysY,
		Spacer:           19,
		ButtonSize:       dc.KeySize,
		ImageFormat:      dc.ImageFmt,
		ImageRotate:      dc.Rotate,
		ConvertKey:       dc.Convert,
	}
}

type Event struct {
	Kind  int
	Index int
	At    time.Time
}

const (
	EventKeyPressed  = 1
	EventKeyReleased = 2
)

type Deck struct {
	sd     *streamdeck.StreamDeck
	cfg    DeckConfig
	serial string

	mu         sync.Mutex
	events     chan Event
	closed     bool
	brightness int
}

func OpenDeck(serial string) (*Deck, error) {
	desiredSerial := serial
	if serial == "first" || serial == "" {
		desiredSerial = ""
	}

	sd, err := streamdeck.NewStreamDeck(desiredSerial)
	if err != nil {
		return nil, fmt.Errorf("opening streamdeck: %w", err)
	}

	cfg, ok := findConfig(sd.Config.ProductID)
	if !ok {
		sd.Close()
		return nil, fmt.Errorf("unsupported device PID: 0x%04x", sd.Config.ProductID)
	}

	d := &Deck{
		sd:         sd,
		cfg:        cfg,
		serial:     sd.Serial(),
		events:     make(chan Event, 64),
		brightness: 75,
	}

	sd.SetBtnEventCb(func(s streamdeck.State, e streamdeck.Event) {
		kind := 0
		switch e.Kind {
		case streamdeck.EventKeyPressed:
			kind = EventKeyPressed
		case streamdeck.EventKeyReleased:
			kind = EventKeyReleased
		default:
			return
		}
		select {
		case d.events <- Event{Kind: kind, Index: e.Which, At: time.Now()}:
		default:
			slog.Warn("event channel full, dropping event", "kind", e.Kind, "index", e.Which)
		}
	})

	slog.Info("deck opened", "serial", d.serial, "model", cfg.Name, "keys", cfg.KeysX*cfg.KeysY)
	return d, nil
}

func (d *Deck) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	close(d.events)
	d.sd.Close()
}

func (d *Deck) Serial() string  { return d.serial }
func (d *Deck) Events() <-chan Event { return d.events }
func (d *Deck) NumKeys() int { return d.cfg.KeysX * d.cfg.KeysY }
func (d *Deck) KeySize() int { return d.cfg.KeySize }
func (d *Deck) Config() DeckConfig { return d.cfg }
func (d *Deck) Model() string { return d.cfg.Name }

type DeckInfo struct {
	Serial  string `json:"serial"`
	Model   string `json:"model"`
	KeysX   int    `json:"keys_x"`
	KeysY   int    `json:"keys_y"`
	NumKeys int    `json:"num_keys"`
	KeySize int    `json:"key_size"`
}

func (d *Deck) DeckInfo() DeckInfo {
	return DeckInfo{
		Serial:  d.serial,
		Model:   d.cfg.Name,
		KeysX:   d.cfg.KeysX,
		KeysY:   d.cfg.KeysY,
		NumKeys: d.cfg.KeysX * d.cfg.KeysY,
		KeySize: d.cfg.KeySize,
	}
}

func OpenAllDecks() ([]*Deck, error) {
	devices := hid.Enumerate(streamdeck.VendorID, 0)
	if len(devices) == 0 {
		return nil, fmt.Errorf("no stream deck devices found")
	}
	var decks []*Deck
	for _, d := range devices {
		if _, ok := findConfig(d.ProductID); !ok {
			slog.Warn("skipping unsupported device", "pid", fmt.Sprintf("0x%04x", d.ProductID))
			continue
		}
		deck, err := OpenDeck(d.Serial)
		if err != nil {
			slog.Warn("failed to open deck", "serial", d.Serial, "error", err)
			continue
		}
		decks = append(decks, deck)
	}
	if len(decks) == 0 {
		return nil, fmt.Errorf("no supported stream deck devices could be opened")
	}
	return decks, nil
}

func (d *Deck) SetBrightness(val int) error {
	if val < 0 {
		val = 0
	}
	if val > 100 {
		val = 100
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("deck closed")
	}
	d.brightness = val
	return d.sd.SetBrightness(uint16(val))
}

func (d *Deck) Brightness() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.brightness
}

func (d *Deck) FillColor(keyIndex int, r, g, b uint8) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("deck closed")
	}
	return d.sd.FillColor(keyIndex, int(r), int(g), int(b))
}

func (d *Deck) FillImage(keyIndex int, img image.Image) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("deck closed")
	}
	return d.sd.FillImage(keyIndex, img)
}

func (d *Deck) FillPanel(img image.Image) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("deck closed")
	}
	return d.sd.FillPanel(img)
}

func (d *Deck) ClearAll() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("deck closed")
	}
	return d.sd.ClearAllBtns()
}

func (d *Deck) ClearKey(idx int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("deck closed")
	}
	return d.sd.ClearBtn(idx)
}

func (d *Deck) WriteText(keyIndex int, text string, bg color.Color, fontName string, fontSize float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("deck closed")
	}
	if fontSize <= 0 {
		fontSize = 18
	}
	font := streamdeck.MonoMedium
	if fontName == "regular" {
		font = streamdeck.MonoRegular
	}

	ks := d.cfg.KeySize
	charW := fontSize * 0.55
	textW := int(float64(len(text)) * charW)
	posX := (ks - textW) / 2
	if posX < 2 {
		posX = 2
	}
	posY := ks/2 - 24 + int(fontSize/3)
	if posY < 2 {
		posY = 2
	}

	return d.sd.WriteText(keyIndex, streamdeck.TextButton{
		Lines: []streamdeck.TextLine{
			{
				Text:      text,
				PosX:      posX,
				PosY:      posY,
				Font:      font,
				FontSize:  fontSize,
				FontColor: color.White,
			},
		},
		BgColor: bg,
	})
}

func (d *Deck) WriteTextOnImage(keyIndex int, img image.Image, text, fontName string, fontSize float64) error {
	ks := d.cfg.KeySize

	g := gift.New(
		gift.Resize(ks, ks, gift.LanczosResampling),
	)
	rgba := image.NewRGBA(image.Rect(0, 0, ks, ks))
	g.Draw(rgba, img)

	lines := []streamdeck.TextLine{}
	if text != "" {
		if fontSize <= 0 {
			fontSize = 16
		}
		font := streamdeck.MonoRegular
		if fontName == "medium" {
			font = streamdeck.MonoMedium
		}

		barHeight := 20
		if ks < 72 {
			barHeight = 18
		}
		barRect := image.Rect(0, ks-barHeight, ks, ks)
		draw.Draw(rgba, barRect, &image.Uniform{color.RGBA{0, 0, 0, 180}}, image.Point{}, draw.Over)

		offsetY := 24
		baselineY := ks - 6
		posY := baselineY - offsetY
		if posY < 2 {
			posY = 2
		}

		charW := fontSize * 0.55
		textW := int(float64(len(text)) * charW)
		posX := (ks - textW) / 2
		if posX < 2 {
			posX = 2
		}

		lines = append(lines, streamdeck.TextLine{
			Text:      text,
			PosX:      posX,
			PosY:      posY,
			Font:      font,
			FontSize:  fontSize,
			FontColor: color.White,
		})
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("deck closed")
	}
	return d.sd.WriteTextOnImage(keyIndex, rgba, lines)
}
