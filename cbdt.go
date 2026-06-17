package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"log/slog"
	"sync"

	_ "image/jpeg"

	"github.com/disintegration/gift"
)

type cbdtIndex struct {
	once   sync.Once
	imgs   []image.Image
	scale  int
}

var emojiCBDT cbdtIndex

func loadCBDT(data []byte) ([]byte, []byte, error) {
	if len(data) < 12 {
		return nil, nil, fmt.Errorf("font too small")
	}
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	off := 12
	var cmapData, cbdtData []byte
	for i := 0; i < numTables; i++ {
		if off+16 > len(data) {
			break
		}
		tag := string(data[off : off+4])
		tblOff := int(binary.BigEndian.Uint32(data[off+8 : off+12]))
		tblLen := int(binary.BigEndian.Uint32(data[off+12 : off+16]))
		switch tag {
		case "cmap":
			if tblOff+tblLen <= len(data) {
				cmapData = data[tblOff : tblOff+tblLen]
			}
		case "CBDT":
			if tblOff+tblLen <= len(data) {
				cbdtData = data[tblOff : tblOff+tblLen]
			}
		}
		off += 16
	}
	if cmapData == nil {
		return nil, nil, fmt.Errorf("cmap table not found")
	}
	if cbdtData == nil {
		return nil, nil, fmt.Errorf("CBDT table not found")
	}
	return cmapData, cbdtData, nil
}

func cmapGlyphIndex(cmap []byte, r rune) (int, bool) {
	if len(cmap) < 4 {
		return 0, false
	}
	numTables := int(binary.BigEndian.Uint16(cmap[2:4]))
	for i := 0; i < numTables; i++ {
		boff := 4 + i*8
		if boff+8 > len(cmap) {
			break
		}
		platform := binary.BigEndian.Uint16(cmap[boff : boff+2])
		encoding := binary.BigEndian.Uint16(cmap[boff+2 : boff+4])
		if platform != 3 || encoding != 10 {
			continue
		}
		subOff := int(binary.BigEndian.Uint32(cmap[boff+4 : boff+8]))
		if subOff+2 > len(cmap) {
			continue
		}
		fmtRaw := binary.BigEndian.Uint16(cmap[subOff:])
		if fmtRaw != 12 {
			continue
		}
		return cmapFormat12(cmap[subOff:], r)
	}
	return 0, false
}

func cmapFormat12(data []byte, r rune) (int, bool) {
	if len(data) < 16 {
		return 0, false
	}
	numGroups := int(binary.BigEndian.Uint32(data[12:16]))
	cp := uint32(r)
	lo, hi := 0, numGroups-1
	for lo <= hi {
		mid := (lo + hi) / 2
		goff := 16 + mid*12
		if goff+12 > len(data) {
			break
		}
		start := binary.BigEndian.Uint32(data[goff:])
		end := binary.BigEndian.Uint32(data[goff+4:])
		startGlyph := binary.BigEndian.Uint32(data[goff+8:])
		if cp < start {
			hi = mid - 1
		} else if cp > end {
			lo = mid + 1
		} else {
			return int(startGlyph + (cp - start)), true
		}
	}
	return 0, false
}

func scanCBDTImages(cbdt []byte, firstGlyph int) ([]image.Image, error) {
	if len(cbdt) < 4 {
		return nil, fmt.Errorf("CBDT too small")
	}
	pos := 4
	var imgs []image.Image
	for pos < len(cbdt) {
		if pos+9 > len(cbdt) {
			break
		}
		dataSize := int(binary.BigEndian.Uint16(cbdt[pos+7 : pos+9]))
		pngOff := pos + 9
		if pngOff+dataSize > len(cbdt) {
			break
		}
		img, err := png.Decode(bytes.NewReader(cbdt[pngOff : pngOff+dataSize]))
		if err != nil {
			pos += 9 + dataSize
			continue
		}
		imgs = append(imgs, img)
		pos += 9 + dataSize
	}
	if len(imgs) == 0 {
		return nil, fmt.Errorf("no valid PNG images found in CBDT")
	}
	slog.Debug("CBDT scan", "images", len(imgs), "firstGlyph", firstGlyph)
	return imgs, nil
}

func renderCBDTGlyph(r rune, targetSize int, scale float64) (image.Image, bool) {
	fontData := loadColorEmojiFont()
	if fontData == nil {
		return nil, false
	}
	cmap, cbdt, err := loadCBDT(fontData)
	if err != nil {
		slog.Debug("CBDT load", "error", err)
		return nil, false
	}
	gid, ok := cmapGlyphIndex(cmap, r)
	if !ok {
		return nil, false
	}
	emojiCBDT.once.Do(func() {
		imgs, err := scanCBDTImages(cbdt, 4)
		if err != nil {
			slog.Warn("CBDT scan", "error", err)
			return
		}
		emojiCBDT.imgs = imgs
	})
	if emojiCBDT.imgs == nil {
		return nil, false
	}
	idx := gid - 5
	if idx < 0 || idx >= len(emojiCBDT.imgs) {
		return nil, false
	}
	raw := emojiCBDT.imgs[idx]

	displaySize := int(float64(targetSize) * scale)
	if displaySize > targetSize {
		displaySize = targetSize
	}
	if displaySize < 1 {
		displaySize = 1
	}

	scaled := raw
	if raw.Bounds().Dx() != displaySize || raw.Bounds().Dy() != displaySize {
		g := gift.New(gift.Resize(displaySize, displaySize, gift.LanczosResampling))
		rgba := image.NewRGBA(image.Rect(0, 0, displaySize, displaySize))
		g.Draw(rgba, raw)
		scaled = rgba
	}

	out := image.NewRGBA(image.Rect(0, 0, targetSize, targetSize))

	offX := (targetSize - scaled.Bounds().Dx()) / 2
	offY := (targetSize - scaled.Bounds().Dy()) / 2
	rect := image.Rect(offX, offY, offX+scaled.Bounds().Dx(), offY+scaled.Bounds().Dy())
	draw.Draw(out, rect, scaled, image.Point{}, draw.Over)

	return out, true
}

func scaleToTarget(img image.Image, targetSize int) image.Image {
	b := img.Bounds()
	if b.Dx() == targetSize && b.Dy() == targetSize {
		return img
	}
	g := gift.New(gift.Resize(targetSize, targetSize, gift.LanczosResampling))
	rgba := image.NewRGBA(image.Rect(0, 0, targetSize, targetSize))
	g.Draw(rgba, img)
	return rgba
}
