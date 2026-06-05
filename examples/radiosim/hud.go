package main

import (
	"fmt"
	"image"
	"image/color"
	"math/cmplx"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// HUD caches the last-rendered panel key so the panel image is only re-rasterized
// and re-uploaded when its contents actually change (rebaking every frame is
// wasteful).
type HUD struct{ lastKey string }

const (
	hudW = 300
	hudH = 204
)

// updateHUD rebuilds the receiver readout panel when its values change, then
// queues it to be drawn at the top-left of the window.
func updateHUD(c *app.Ctx, scene *Scene, live *Live) {
	hud := app.GetResource[HUD](c)
	if hud == nil {
		return
	}
	dBm := sim.DBm(live.PowerW)
	spreadNs := sim.DelaySpread(live.Taps) * 1e9
	dist := live.Rx.Pos.Sub(scene.Tx.Pos).Length()
	snr := sim.SNRdB(live.PowerW, live.Rx)
	ber := sim.BER(snr, live.Rx.Mod)
	key := fmt.Sprintf("%.1f|%d|%.1f|%.1f|%.1f|%.1e", dBm, len(live.Paths), spreadNs, dist, snr, ber)
	if key != hud.lastKey {
		img := buildHUDImage(scene, live, dBm, spreadNs, float64(dist), snr, ber)
		if err := render3d.LoadPanel(c, "hud", img); err == nil {
			hud.lastKey = key
		}
	}
	render3d.DrawPanel(c, "hud", 12, 12, hudW, hudH)
}

// buildHUDImage rasterizes the receiver panel: a translucent dark card with the
// received power, link-quality (SNR/BER), path/spread/distance readouts, and a
// multipath delay-power bar chart.
func buildHUDImage(scene *Scene, live *Live, dBm, spreadNs, dist, snr, ber float64) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, hudW, hudH))
	fillRect(img, 0, 0, hudW, hudH, color.RGBA{12, 16, 24, 205})
	// Accent top bar.
	fillRect(img, 0, 0, hudW, 3, color.RGBA{80, 200, 255, 255})

	white := color.RGBA{235, 240, 245, 255}
	dim := color.RGBA{150, 165, 180, 255}
	accent := color.RGBA{120, 220, 255, 255}

	drawText(img, 12, 22, "RECEIVER", accent)
	drawText(img, 12, 44, fmt.Sprintf("Pr   %7.1f dBm", dBm), white)
	drawText(img, 12, 60, fmt.Sprintf("SNR  %7.1f dB", snr), white)
	drawText(img, 12, 76, fmt.Sprintf("BER  %9.1e  (%s)", ber, modName(live.Rx.Mod)), dim)
	drawText(img, 12, 92, fmt.Sprintf("dist %7.1f m", dist), dim)
	drawText(img, 12, 108, fmt.Sprintf("paths %d   freq %.0f MHz", len(live.Paths), scene.Tx.FreqHz/1e6), dim)
	drawText(img, 12, 124, fmt.Sprintf("delay spread %.1f ns", spreadNs), dim)

	drawText(img, 12, 150, "MULTIPATH PROFILE", accent)
	drawDelayProfile(img, live.Taps, 12, 158, hudW-24, 38)
	return img
}

// modName is a short label for a modulation scheme.
func modName(mod sim.Modulation) string {
	switch mod {
	case sim.BPSK:
		return "BPSK"
	case sim.QAM16:
		return "16-QAM"
	case sim.QAM64:
		return "64-QAM"
	default:
		return "QPSK"
	}
}

// drawDelayProfile draws a small bar chart of tap power vs delay in the pixel rect
// (x,y,w,h), origin at the rect's top-left.
func drawDelayProfile(img *image.RGBA, taps []sim.Tap, x, y, w, h int) {
	fillRect(img, x, y, w, h, color.RGBA{8, 10, 14, 255})
	if len(taps) == 0 {
		return
	}
	maxDelay := float64(taps[len(taps)-1].Delay)
	if maxDelay <= 0 {
		maxDelay = 1e-9
	}
	maxP := 0.0
	for _, t := range taps {
		if p := cmplx.Abs(t.Amp); p > maxP {
			maxP = p
		}
	}
	if maxP <= 0 {
		return
	}
	for _, t := range taps {
		fx := float64(t.Delay) / maxDelay
		bx := x + 2 + int(fx*float64(w-6))
		p := cmplx.Abs(t.Amp) / maxP
		bh := int(p * float64(h-4))
		if bh < 1 {
			bh = 1
		}
		col := color.RGBA{90, 210, 255, 255}
		fillRect(img, bx, y+h-2-bh, 2, bh, col)
	}
}

// drawText draws a string at baseline (x,y) using the built-in 7x13 bitmap font.
func drawText(img *image.RGBA, x, y int, s string, col color.Color) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

// fillRect fills a solid rectangle, clipped to the image bounds.
func fillRect(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	b := img.Bounds()
	for yy := y; yy < y+h; yy++ {
		if yy < b.Min.Y || yy >= b.Max.Y {
			continue
		}
		for xx := x; xx < x+w; xx++ {
			if xx < b.Min.X || xx >= b.Max.X {
				continue
			}
			img.SetRGBA(xx, yy, col)
		}
	}
}
