package main

import (
	"image"
	"image/color"
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	scopeW, scopeH = 460, 150
	margin         = 16
)

// plotsSystem draws the genuinely raster content — the receiver oscilloscope and
// the constellation panels — via the render3d overlay2d image path, which suits
// computed plots. The structured readout/config UI and the node editor moved to
// Dear ImGui (hudUI / editorUI). Registered as a pre-render system so the panels
// reflect this frame's state.
func plotsSystem(c *app.Ctx) {
	ui := app.GetResource[UI](c)
	if ui != nil && ui.Mode == ModeGraph {
		return // the ImGui node editor owns the screen in graph mode
	}

	lab := app.GetResource[Lab](c)
	field := app.GetResource[Field](c)
	if field != nil {
		drawScope(c, field)
	}
	if lab != nil {
		drawConstellations(c, lab)
	}
}

// drawConstellations renders, along the bottom, a single constellation panel for
// the current selection: the selected transmitter's ideal constellation (with its
// current symbol lit) while it is sending, or the selected receiver's received
// scatter and decoded text. Nothing is shown when nothing is selected.
func drawConstellations(c *app.Ctx, lab *Lab) {
	_, fbH := windowSize(c)
	const sz = 220
	x := margin + scopeW + 24
	y := fbH - sz - margin
	switch lab.Sel {
	case SelTx:
		txd := selectedTx(c, lab)
		if txd == nil || txd.Play == nil || !txd.Play.Playing || len(txd.Play.Symbols) == 0 {
			return
		}
		render3d.LoadPanel(c, "const_tx", buildTxConst(txd.Play))
		render3d.DrawPanel(c, "const_tx", x, y, sz, sz)
	case SelRx:
		rxd := selectedRx(c, lab)
		if rxd == nil || rxd.Decode == nil {
			return
		}
		render3d.LoadPanel(c, "const_rx", buildRxConst(rxGraphMod(rxd.Graph), rxd.Decode))
		render3d.DrawPanel(c, "const_rx", x, y, sz, sz)
	}
}

// constBase rasterizes the shared constellation backdrop — titled panel, axes, and
// the faint ideal grid for mod — and returns it with the plot center and scale so
// the caller can overlay its own points.
func constBase(mod sim.Modulation, title string, bar color.RGBA) (*image.RGBA, int, int, float64) {
	const sz = 220
	img := image.NewRGBA(image.Rect(0, 0, sz, sz))
	fillRect(img, 0, 0, sz, sz, color.RGBA{10, 14, 20, 215})
	fillRect(img, 0, 0, sz, 4, bar)
	drawText(img, 12, 14, title, bar, 2)

	cx, cy := sz/2, sz/2+14
	scale := float64(sz/2-30) / 1.5
	drawConstellationGrid(img, cx, cy, scale, sz/2-16, (sz-56)/2, mod)
	return img, cx, cy, scale
}

// drawConstellationGrid draws the crosshair axes and the faint ideal symbol points
// for mod, centered at (cx,cy) with the given per-unit scale and axis half-extents.
// Shared by the HUD constellation panels and the graph's Constellation node so the
// two always show the same constellation.
func drawConstellationGrid(img *image.RGBA, cx, cy int, scale float64, halfW, halfH int, mod sim.Modulation) {
	axis := color.RGBA{40, 50, 60, 255}
	fillRect(img, cx-halfW, cy, 2*halfW, 1, axis)
	fillRect(img, cx, cy-halfH, 1, 2*halfH, axis)
	plotConstellation(img, cx, cy, scale, mod)
}

// plotConstellation draws mod's ideal symbol points as faint dots centered at
// (cx,cy) with the given per-unit scale.
func plotConstellation(img *image.RGBA, cx, cy int, scale float64, mod sim.Modulation) {
	for _, p := range constellation(mod) {
		drawDot(img, cx+int(real(p)*scale), cy-int(imag(p)*scale), 2, color.RGBA{70, 90, 110, 255})
	}
}

// buildTxConst draws the transmitter's ideal constellation with its current symbol
// highlighted.
func buildTxConst(play *Play) image.Image {
	img, cx, cy, scale := constBase(play.Mod, "TX  "+modName(play.Mod), color.RGBA{120, 220, 255, 255})
	if play.CurTx >= 0 && play.CurTx < len(play.Symbols) {
		s := play.Symbols[play.CurTx]
		drawDot(img, cx+int(real(s)*scale), cy-int(imag(s)*scale), 5, color.RGBA{255, 230, 120, 255})
	}
	return img
}

// buildRxConst draws the receiver's noisy received scatter (sampled from the wave)
// over the ideal grid for the receiver's chosen demodulation, plus the message
// decoded so far (which fills in over each loop and errors at low SNR).
func buildRxConst(mod sim.Modulation, dec *Decode) image.Image {
	const sz = 220
	img, cx, cy, scale := constBase(mod, "RX  "+modName(mod), color.RGBA{150, 255, 190, 255})
	var buf [rxBufLen]complex128
	n := dec.rxPoints(buf[:])
	for i := 0; i < n; i++ {
		p := buf[i]
		drawDot(img, cx+int(real(p)*scale), cy-int(imag(p)*scale), 1, color.RGBA{130, 255, 175, 200})
	}
	drawText(img, 10, sz-20, clip("> "+dec.text(), 24), color.RGBA{170, 255, 200, 255}, 2)
	return img
}

// drawDot fills a small square centered at (cx,cy) with half-extent rad.
func drawDot(img *image.RGBA, cx, cy, rad int, col color.RGBA) {
	fillRect(img, cx-rad, cy-rad, 2*rad+1, 2*rad+1, col)
}

// drawScope renders the receiver's oscilloscope (the field sampled at the Rx over
// time) at the bottom-left. It is rebuilt every frame since the trace is live.
func drawScope(c *app.Ctx, field *Field) {
	_, h := windowSize(c)
	render3d.LoadPanel(c, "scope", buildScope(field))
	render3d.DrawPanel(c, "scope", margin, h-scopeH-margin, scopeW, scopeH)
}

func buildScope(field *Field) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, scopeW, scopeH))
	fillRect(img, 0, 0, scopeW, scopeH, color.RGBA{10, 18, 14, 210})
	fillRect(img, 0, 0, scopeW, 5, color.RGBA{120, 255, 170, 255})

	accent := color.RGBA{120, 255, 170, 255}
	grid := color.RGBA{40, 60, 50, 255}
	trace := color.RGBA{150, 255, 190, 255}

	drawText(img, 16, 22, "RX SIGNAL", accent, 2)

	// Plot area and zero line.
	const pad = 16
	top, bot := 40, scopeH-pad
	mid := (top + bot) / 2
	fillRect(img, pad, mid, scopeW-2*pad, 1, grid)

	var buf [scopeLen]float64
	field.ordered(buf[:])

	// Auto-scale (AGC): normalize to the buffer's peak so the trace reads well at
	// any transmit power. What remains visible is the signal-to-noise ratio — a
	// clean sine at high SNR, fuzz burying the wave at low SNR.
	maxAbs := 1e-30
	for i := 0; i < scopeLen; i++ {
		if a := math.Abs(buf[i]); a > maxAbs {
			maxAbs = a
		}
	}

	plotW := scopeW - 2*pad
	half := float64(bot-top) / 2 * 0.92
	px, py := -1, -1
	for i := 0; i < scopeLen; i++ {
		x := pad + i*plotW/scopeLen
		y := mid - int(buf[i]/maxAbs*half)
		if px >= 0 {
			plotLine(img, px, py, x, y, trace)
		}
		px, py = x, y
	}
	return img
}

// plotLine draws a 2px-thick line between two points (Bresenham).
func plotLine(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx, sy := sign(x1-x0), sign(y1-y0)
	err := dx + dy
	for {
		img.SetRGBA(x0, y0, col)
		img.SetRGBA(x0, y0+1, col)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	default:
		return 0
	}
}

// windowSize returns the current framebuffer size in pixels (not logical points),
// which is the coordinate space DrawPanel uses — they differ on Retina/HiDPI
// displays. Falls back to the configured default.
func windowSize(c *app.Ctx) (int, int) {
	if win := render3d.Window(c); win != nil {
		return win.GetFramebufferSize()
	}
	return 1280, 720
}

// drawText draws s with its top-left at (x,y) using the built-in 7x13 bitmap font
// nearest-neighbor upscaled by an integer factor, so the text stays crisp at any
// size. A scale of 1 is the native font size.
func drawText(dst *image.RGBA, x, y int, s string, col color.Color, scale int) {
	if scale < 1 {
		scale = 1
	}
	// Rasterize once at native size into a tight scratch image.
	const adv, asc, h = 7, 11, 14
	w := len(s)*adv + 1
	if w < 1 {
		return
	}
	tmp := image.NewRGBA(image.Rect(0, 0, w, h))
	d := &font.Drawer{
		Dst:  tmp,
		Src:  image.NewUniform(col),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(0, asc),
	}
	d.DrawString(s)

	// Blit, expanding each opaque source texel into a scale×scale block.
	b := dst.Bounds()
	for ty := 0; ty < h; ty++ {
		for tx := 0; tx < w; tx++ {
			if tmp.RGBAAt(tx, ty).A == 0 {
				continue
			}
			cc := tmp.RGBAAt(tx, ty)
			px := x + tx*scale
			py := y + ty*scale
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					xx, yy := px+dx, py+dy
					if xx >= b.Min.X && xx < b.Max.X && yy >= b.Min.Y && yy < b.Max.Y {
						dst.SetRGBA(xx, yy, cc)
					}
				}
			}
		}
	}
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
