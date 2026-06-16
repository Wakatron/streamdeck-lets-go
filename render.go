package main

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/disintegration/gift"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"streamdeck-lets-go/internal/config"
)

func RenderKeyToImage(k *config.KeyConfig, keySize int) image.Image {
	if k == nil {
		return blankImage(keySize, color.RGBA{0, 0, 0, 255})
	}
	if k.Icon == "" && k.Label == "" {
		if k.Background != "" {
			if bg, err := parseHexColor(k.Background); err == nil {
				return blankImage(keySize, bg)
			}
		}
		return blankImage(keySize, color.RGBA{64, 64, 64, 255})
	}

	faScale := 0.55
	if k.IconScale != nil {
		faScale = *k.IconScale
	}

	if k.Icon != "" {
		img, err := loadImage(k.Icon, keySize, faScale)
		if err != nil {
			img = blankImage(keySize, color.RGBA{64, 64, 64, 255})
		}
		if k.Background != "" {
			if bg, err := parseHexColor(k.Background); err == nil {
				img = applyBackground(img, bg)
			}
		}
		if k.Label != "" {
			return composeImageWithLabel(img, k.Label, keySize)
		}
		g := gift.New(gift.Resize(keySize, keySize, gift.LanczosResampling))
		rgba := image.NewRGBA(image.Rect(0, 0, keySize, keySize))
		g.Draw(rgba, img)
		return rgba
	}

	if k.Background != "" {
		if bg, err := parseHexColor(k.Background); err == nil {
			if k.Label != "" {
				fontSize := 12.0
				if k.FontSize != nil {
					fontSize = *k.FontSize
				}
				return renderUnicodeText(k.Label, fontSize, keySize, bg, color.White)
			}
			return blankImage(keySize, bg)
		}
	}
	return renderTextImage(k.Label, keySize)
}

func blankImage(size int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
	return img
}

func composeImageWithLabel(src image.Image, text string, keySize int) image.Image {
	g := gift.New(gift.Resize(keySize, keySize, gift.LanczosResampling))
	rgba := image.NewRGBA(image.Rect(0, 0, keySize, keySize))
	g.Draw(rgba, src)

	barHeight := 20
	if keySize < 72 {
		barHeight = 18
	}
	barRect := image.Rect(0, keySize-barHeight, keySize, keySize)
	draw.Draw(rgba, barRect, &image.Uniform{color.RGBA{0, 0, 0, 180}}, image.Point{}, draw.Over)

	if text != "" {
		face := basicfont.Face7x13
		textW := font.MeasureString(face, text).Ceil()
		posX := (keySize - textW) / 2
		if posX < 2 {
			posX = 2
		}
		posY := keySize - barHeight/2 + face.Metrics().Height.Ceil()/2
		if posY >= keySize {
			posY = keySize - 2
		}

		d := &font.Drawer{
			Dst:  rgba,
			Src:  image.NewUniform(color.White),
			Face: face,
			Dot:  fixed.P(posX, posY),
		}
		d.DrawString(text)
	}

	return rgba
}

func renderTextImage(text string, keySize int) image.Image {
	rgba := blankImage(keySize, color.RGBA{0, 0, 0, 255}).(*image.RGBA)

	if text != "" {
		face := basicfont.Face7x13
		textW := font.MeasureString(face, text).Ceil()
		posX := (keySize - textW) / 2
		if posX < 2 {
			posX = 2
		}
		posY := keySize/2 + face.Metrics().Height.Ceil()/2

		d := &font.Drawer{
			Dst:  rgba,
			Src:  image.NewUniform(color.White),
			Face: face,
			Dot:  fixed.P(posX, posY),
		}
		d.DrawString(text)
	}

	return rgba
}
