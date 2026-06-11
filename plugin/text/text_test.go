package text_test

import (
	"image/color"
	"testing"

	"github.com/hvuhsg/spliti/plugin/text"
)

func TestRenderProducesOpaquePixelsOfTheGivenColor(t *testing.T) {
	face := text.Default(24)
	img := face.Render("Hello", color.RGBA{R: 255, G: 0, B: 0, A: 255})

	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		t.Fatalf("rendered image is empty: %v", b)
	}

	var inked, redInked int
	for i := 0; i < len(img.Pix); i += 4 {
		if a := img.Pix[i+3]; a > 0 {
			inked++
			if img.Pix[i+0] > 0 && img.Pix[i+1] == 0 && img.Pix[i+2] == 0 {
				redInked++
			}
		}
	}
	if inked == 0 {
		t.Fatal("no pixels were inked")
	}
	if redInked != inked {
		t.Errorf("%d of %d inked pixels are not red", inked-redInked, inked)
	}
}

func TestMeasureMatchesRenderAndHandlesMultiline(t *testing.T) {
	face := text.Default(16)

	w1, h1 := face.Measure("line")
	w2, h2 := face.Measure("line\nlonger line")
	if h2 != 2*h1 {
		t.Errorf("two-line height = %d, want twice one-line %d", h2, h1)
	}
	if w2 <= w1 {
		t.Errorf("width of widest line = %d, want > %d", w2, w1)
	}

	img := face.Render("line\nlonger line", color.White)
	if img.Bounds().Dx() != w2 || img.Bounds().Dy() != h2 {
		t.Errorf("rendered size %v, want %dx%d", img.Bounds(), w2, h2)
	}
}

func TestRenderEmptyStringIsNonZeroSized(t *testing.T) {
	face := text.Mono(14)
	img := face.Render("", color.White)
	if img.Bounds().Dx() < 1 || img.Bounds().Dy() < 1 {
		t.Fatalf("empty render has zero dimension: %v", img.Bounds())
	}
}
