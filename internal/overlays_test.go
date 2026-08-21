package internal

import (
	"image"
	"image/color"
	"testing"
)

func TestDrawOverlayBanner(t *testing.T) {
	src := rectImg{w: 100, h: 150}
	out := drawOverlayBanner(src, "Leaving Soon", "IN 3 DAYS", overlayStyle{
		BarColor:      color.RGBA{R: 180, G: 30, B: 30, A: 210},
		PillColor:     color.RGBA{R: 20, G: 20, B: 20, A: 220},
		PillTextColor: color.RGBA{R: 255, G: 220, B: 80, A: 255},
	})
	b := out.Bounds()
	if b.Dx() != 100 || b.Dy() != 150 {
		t.Fatalf("unexpected bounds %v", b)
	}
}

type rectImg struct {
	w, h int
}

func (r rectImg) ColorModel() color.Model { return color.RGBAModel }
func (r rectImg) Bounds() image.Rectangle { return image.Rect(0, 0, r.w, r.h) }
func (r rectImg) At(x, y int) color.Color { return color.White }
