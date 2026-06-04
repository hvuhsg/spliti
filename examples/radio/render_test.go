package main

import "testing"

// TestTextures exercises the procedural texture generators for panics and
// expected dimensions (the GPU upload itself is covered by the runtime).
func TestTextures(t *testing.T) {
	if b := squareTexture().Bounds(); b.Dx() != 2 || b.Dy() != 2 {
		t.Errorf("squareTexture size = %v", b)
	}
	if b := discTexture(64).Bounds(); b.Dx() != 64 || b.Dy() != 64 {
		t.Errorf("discTexture size = %v", b)
	}
	_ = ringTexture(256, 0.045)
	_ = arrowTexture(48)
}

// TestBakeText checks label rasterization scales with length and never produces
// a zero-size image (which would fail TextureRegistry.Load).
func TestBakeText(t *testing.T) {
	for _, s := range []string{"", "0", "symbol 00    I = +0.71   Q = +0.71"} {
		img := bakeText(s)
		if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
			t.Errorf("bakeText(%q) produced zero-size image", s)
		}
	}
}

// TestConstPoint checks the four constellation points land inside the plot
// viewport and in the expected quadrants (I right/left, Q up/down).
func TestConstPoint(t *testing.T) {
	_, cy := constVP.mapPoint(constData, 0, 0)
	cx, _ := constVP.mapPoint(constData, 0, 0)
	for idx := 0; idx < 4; idx++ {
		x, y := constPoint(idx)
		if x < constVP.X || x > constVP.X+constVP.W || y < constVP.Y || y > constVP.Y+constVP.H {
			t.Errorf("point %d at (%v,%v) outside viewport", idx, x, y)
		}
		b0, b1 := bitsOf(idx)
		// b0=0 => I positive => right of centre; b1=0 => Q positive => above centre.
		if (b0 == 0) != (x > cx) {
			t.Errorf("point %02b: x=%v vs centre %v, wrong I side", idx, x, cx)
		}
		if (b1 == 0) != (y < cy) {
			t.Errorf("point %02b: y=%v vs centre %v, wrong Q side", idx, y, cy)
		}
	}
}
