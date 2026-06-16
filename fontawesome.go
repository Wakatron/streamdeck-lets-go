package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func parseFAIcon(ref string) (faStyle, string, error) {
	if !strings.HasPrefix(ref, "fa") {
		return 0, "", fmt.Errorf("not a font awesome ref")
	}

	var style faStyle
	var name string

	switch {
	case strings.HasPrefix(ref, "fab:"):
		style = faBrands
		name = strings.TrimPrefix(ref, "fab:")
	case strings.HasPrefix(ref, "far:"):
		style = faRegular
		name = strings.TrimPrefix(ref, "far:")
	case strings.HasPrefix(ref, "fa:"):
		style = faSolid
		name = strings.TrimPrefix(ref, "fa:")
	default:
		return 0, "", fmt.Errorf("invalid font awesome ref: %s", ref)
	}

	if name == "" {
		return 0, "", fmt.Errorf("empty icon name")
	}

	return style, name, nil
}

func faCodepoint(style faStyle, name string) (rune, error) {
	m, ok := faCodepoints[style]
	if !ok {
		return 0, fmt.Errorf("unknown style")
	}

	if cp, ok := m[name]; ok {
		return cp, nil
	}

	if cp, ok := m[normalizeFAName(name)]; ok {
		return cp, nil
	}

	return 0, fmt.Errorf("icon %q not found", name)
}

func normalizeFAName(name string) string {
	if len(name) == 0 {
		return name
	}
	parts := strings.Split(name, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	camel := strings.Join(parts, "")
	return strings.ToLower(camel[:1]) + camel[1:]
}

func faFontBytes(style faStyle) ([]byte, error) {
	return faFonts.ReadFile(style.otfPath())
}

func loadFAFace(style faStyle, pointSize float64) (font.Face, error) {
	data, err := faFontBytes(style)
	if err != nil {
		return nil, fmt.Errorf("read font: %w", err)
	}

	fnt, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse font: %w", err)
	}

	face, err := opentype.NewFace(fnt, &opentype.FaceOptions{
		Size:    pointSize,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("new face: %w", err)
	}
	return face, nil
}

func renderFAGlyph(style faStyle, name string, size int, scale float64) (image.Image, error) {
	cp, err := faCodepoint(style, name)
	if err != nil {
		return nil, err
	}

	if scale <= 0 {
		scale = 0.55
	}
	fontSize := float64(size) * scale
	face, err := loadFAFace(style, fontSize)
	if err != nil {
		return nil, err
	}
	defer face.Close()

	rgba := image.NewRGBA(image.Rect(0, 0, size, size))

	offX := (size - int(fontSize)) / 2
	if offX < 0 {
		offX = size / 8
	}
	baselineY := size * 7 / 10

	d := font.Drawer{
		Dst:  rgba,
		Src:  image.NewUniform(color.White),
		Face: face,
		Dot:  fixed.P(offX, baselineY),
	}
	d.DrawString(string(cp))

	return rgba, nil
}

func isFAIconRef(path string) bool {
	return strings.HasPrefix(path, "fa:") || strings.HasPrefix(path, "far:") || strings.HasPrefix(path, "fab:")
}

