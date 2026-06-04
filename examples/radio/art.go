package main

import (
	"image"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
)

// Shared white textures loaded once at startup under stable refs. Everything is
// white so the Color tint is the visible colour. Axis-aligned rectangles and
// lines are drawn by stretching "sq"; only shapes with curved or diagonal edges
// ("dot", "ring", "arrow") need to be rasterized.

// loadSharedTextures uploads the reusable textures. Call from a Startup system.
func loadSharedTextures(c *app.Ctx) {
	reg := app.GetResource[webgpu.TextureRegistry](c)
	must := func(ref string, img image.Image) {
		if err := reg.Load(ref, img); err != nil {
			panic(err)
		}
	}
	must("sq", squareTexture())
	must("dot", discTexture(64))
	must("ring", ringTexture(256, 0.045))
	must("arrow", arrowTexture(48))
}

// squareTexture is a 2x2 opaque white square — stretched into rectangles, panels
// and axis-aligned lines.
func squareTexture() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	return img
}

// discTexture is an anti-aliased white disc on transparent — the curve/point dot.
func discTexture(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	cx, cy := r-0.5, r-0.5
	const ss = 4
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			inside := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss - 0.5
					py := float64(y) + (float64(sy)+0.5)/ss - 0.5
					dx, dy := px-cx, py-cy
					if dx*dx+dy*dy <= r*r {
						inside++
					}
				}
			}
			i := (y*size + x) * 4
			img.Pix[i+0], img.Pix[i+1], img.Pix[i+2] = 255, 255, 255
			img.Pix[i+3] = uint8(255 * inside / (ss * ss))
		}
	}
	return img
}

// ringTexture is an anti-aliased white circle outline (the QPSK unit circle).
// thickness is a fraction of the radius.
func ringTexture(size int, thickness float64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	r := float64(size)/2 - 1
	cx, cy := float64(size)/2-0.5, float64(size)/2-0.5
	inner := r * (1 - thickness*2)
	const ss = 4
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			cover := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss - 0.5
					py := float64(y) + (float64(sy)+0.5)/ss - 0.5
					dd := (px-cx)*(px-cx) + (py-cy)*(py-cy)
					if dd <= r*r && dd >= inner*inner {
						cover++
					}
				}
			}
			i := (y*size + x) * 4
			img.Pix[i+0], img.Pix[i+1], img.Pix[i+2] = 255, 255, 255
			img.Pix[i+3] = uint8(255 * cover / (ss * ss))
		}
	}
	return img
}

// arrowTexture is a right-pointing white triangle on transparent — the flow
// arrowhead between pipeline stages.
func arrowTexture(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Triangle with apex at the right-centre, base on the left edge.
			fx := float64(x) / float64(size-1)     // 0..1 across
			fy := float64(y)/float64(size-1)*2 - 1 // -1..1 vertical
			inside := fx <= 1 && (fx >= 0) && (abs(fy) <= 1-fx)
			i := (y*size + x) * 4
			img.Pix[i+0], img.Pix[i+1], img.Pix[i+2] = 255, 255, 255
			if inside {
				img.Pix[i+3] = 255
			}
		}
	}
	return img
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
