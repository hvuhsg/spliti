package main

import (
	"image"
	"image/color"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// The engine has no text rendering, so we rasterize strings to a white-on-
// transparent texture with the built-in bitmap font and draw them as ordinary
// (tintable) sprites. basicfont is monospace, so a string's pixel size depends
// only on its length — handy for in-place updates of fixed-width readouts.

// glyphScale nearest-neighbour upscales the 7x13 bitmap font so labels stay
// crisp when the texture is minified by mips rather than blown up blurry.
const glyphScale = 4

// face advance/height in source (1x) pixels.
const (
	faceW = 7
	faceH = 13
)

// bake1x renders s with basicfont at native size onto a transparent RGBA.
func bake1x(s string) *image.RGBA {
	w := len(s) * faceW
	if w == 0 {
		w = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, w, faceH))
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.White),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(0, basicfont.Face7x13.Ascent),
	}
	d.DrawString(s)
	return img
}

// upscale nearest-neighbour enlarges src by an integer factor.
func upscale(src *image.RGBA, scale int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, sw*scale, sh*scale))
	for y := 0; y < sh*scale; y++ {
		sy := y / scale
		for x := 0; x < sw*scale; x++ {
			dst.SetRGBA(x, y, src.RGBAAt(b.Min.X+x/scale, b.Min.Y+sy))
		}
	}
	return dst
}

// bakeText renders a white-on-transparent label image for s.
func bakeText(s string) *image.RGBA { return upscale(bake1x(s), glyphScale) }

// textAspect is the width/height ratio of a baked label of n characters. Used to
// size a label sprite for a target height without re-measuring the image.
func textAspect(n int) float32 {
	if n == 0 {
		n = 1
	}
	return float32(n*faceW) / float32(faceH)
}

// loadLabel bakes s and uploads it under ref, replacing any previous texture.
func loadLabel(c *app.Ctx, ref, s string) {
	reg := app.GetResource[webgpu.TextureRegistry](c)
	if err := reg.Load(ref, bakeText(s)); err != nil {
		panic(err)
	}
}

// spawnLabel bakes s, uploads it under ref, and spawns a centred label sprite of
// the given height. Width follows the font aspect so text isn't stretched.
func spawnLabel(c *app.Ctx, ref, s string, cx, cy, h float32, col webgpu.Color, z int) {
	loadLabel(c, ref, s)
	w := h * textAspect(len(s))
	spawnSprite(c.Commands(), ref, cx-w/2, cy-h/2, w, h, col, z)
}

// spawnLabelFit bakes s and spawns a centred label that fits within maxW × maxH,
// shrinking the height so the (monospace) string never overflows the width.
func spawnLabelFit(c *app.Ctx, ref, s string, cx, cy, maxW, maxH float32, col webgpu.Color, z int) {
	loadLabel(c, ref, s)
	asp := textAspect(len(s))
	h := maxH
	if h*asp > maxW {
		h = maxW / asp
	}
	w := h * asp
	spawnSprite(c.Commands(), ref, cx-w/2, cy-h/2, w, h, col, z)
}

// spawnLabelLeft is spawnLabel anchored at a left edge (x0) rather than centred.
func spawnLabelLeft(c *app.Ctx, ref, s string, x0, cy, h float32, col webgpu.Color, z int) {
	loadLabel(c, ref, s)
	w := h * textAspect(len(s))
	spawnSprite(c.Commands(), ref, x0, cy-h/2, w, h, col, z)
}
