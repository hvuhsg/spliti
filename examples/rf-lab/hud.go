package main

import (
	"fmt"
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

// HUD caches the last-rendered panel keys so each panel image is only
// re-rasterized and re-uploaded when its contents actually change.
type HUD struct {
	lastReadout string
	lastConfig  string
}

const (
	readoutW, readoutH = 400, 286
	configW            = 420
	scopeW, scopeH     = 460, 150
	margin             = 16
)

// hudSystem rebuilds (when changed) and draws the always-on link readout and, when
// a marker is selected, its config panel as a drawer on the right edge. Registered
// as a pre-render system so the panels reflect this frame's state.
func hudSystem(c *app.Ctx) {
	ui := app.GetResource[UI](c)
	if ui != nil && ui.Mode == ModeGraph {
		drawEditor(c)
		return
	}

	hud := app.GetResource[HUD](c)
	lab := app.GetResource[Lab](c)
	link := app.GetResource[Link](c)
	field := app.GetResource[Field](c)
	if hud == nil || lab == nil || link == nil {
		return
	}
	txd := txDevice(c)
	rxd := rxDevice(c)

	drawReadout(c, hud, lab, link, txd)
	drawConfig(c, hud, lab, txd, rxd)
	if field != nil {
		drawScope(c, field)
	}
	if txd != nil && txd.Play != nil {
		drawConstellations(c, txd.Play, rxd)
	}
}

// drawConstellations renders the ideal TX constellation (current symbol lit) and
// the noisy received scatter (with the decoded text), along the bottom, while a
// message is playing.
func drawConstellations(c *app.Ctx, play *Play, rxd *RxDevice) {
	if !play.Playing || len(play.Symbols) == 0 {
		return
	}
	_, fbH := windowSize(c)
	const sz = 220
	y := fbH - sz - margin
	xTx := margin + scopeW + 24
	xRx := xTx + sz + 16
	decoded := ""
	if rxd != nil && rxd.Decode != nil {
		decoded = rxd.Decode.text()
	}
	render3d.LoadPanel(c, "const_tx", buildConst(play, true, ""))
	render3d.DrawPanel(c, "const_tx", xTx, y, sz, sz)
	render3d.LoadPanel(c, "const_rx", buildConst(play, false, decoded))
	render3d.DrawPanel(c, "const_rx", xRx, y, sz, sz)
}

// buildConst rasterizes a constellation panel. tx=true draws the ideal grid with
// the current symbol highlighted; tx=false draws the faint grid plus the received
// noisy scatter and the decoded text.
func buildConst(play *Play, tx bool, decoded string) image.Image {
	const sz = 220
	img := image.NewRGBA(image.Rect(0, 0, sz, sz))
	fillRect(img, 0, 0, sz, sz, color.RGBA{10, 14, 20, 215})
	bar := color.RGBA{120, 220, 255, 255}
	title := "TX  " + modName(play.Mod)
	if !tx {
		bar = color.RGBA{150, 255, 190, 255}
		title = "RX  " + modName(play.Mod)
	}
	fillRect(img, 0, 0, sz, 4, bar)
	drawText(img, 12, 14, title, bar, 2)

	cx, cy := sz/2, sz/2+14
	scale := float64(sz/2-30) / 1.5
	// axes
	fillRect(img, 16, cy, sz-32, 1, color.RGBA{40, 50, 60, 255})
	fillRect(img, cx, 40, 1, sz-56, color.RGBA{40, 50, 60, 255})

	ideal := constellation(play.Mod)
	for _, p := range ideal {
		px := cx + int(real(p)*scale)
		py := cy - int(imag(p)*scale)
		drawDot(img, px, py, 2, color.RGBA{70, 90, 110, 255})
	}

	if tx {
		if play.CurTx >= 0 && play.CurTx < len(play.Symbols) {
			s := play.Symbols[play.CurTx]
			drawDot(img, cx+int(real(s)*scale), cy-int(imag(s)*scale), 5, color.RGBA{255, 230, 120, 255})
		}
	} else {
		var buf [rxBufLen]complex128
		n := play.rxPoints(buf[:])
		for i := 0; i < n; i++ {
			p := buf[i]
			drawDot(img, cx+int(real(p)*scale), cy-int(imag(p)*scale), 1, color.RGBA{130, 255, 175, 200})
		}
		// Decoded message so far (fills in over each loop; errors at low SNR).
		drawText(img, 10, sz-20, clip("> "+decoded, 24), color.RGBA{170, 255, 200, 255}, 2)
	}
	return img
}

// drawDot fills a small square centered at (cx,cy) with half-extent rad.
func drawDot(img *image.RGBA, cx, cy, rad int, col color.RGBA) {
	fillRect(img, cx-rad, cy-rad, 2*rad+1, 2*rad+1, col)
}

// drawReadout renders and blits the top-left link-budget panel.
func drawReadout(c *app.Ctx, hud *HUD, lab *Lab, link *Link, txd *TxDevice) {
	freq, power := 24e6, 0.0
	if txd != nil {
		freq, power = txd.FreqHz, txd.PowerW
	}
	dBm := sim.DBm(link.PowerW)
	key := fmt.Sprintf("%.1f|%.1f|%.1f|%.0f|%.1f", dBm, link.DistM, link.SNRdB,
		freq/1e6, sim.DBm(power))
	if key != hud.lastReadout {
		if err := render3d.LoadPanel(c, "readout", buildReadout(dBm, link, freq, power)); err == nil {
			hud.lastReadout = key
		}
	}
	render3d.DrawPanel(c, "readout", margin, margin, readoutW, readoutH)
}

func buildReadout(dBm float64, link *Link, freq, power float64) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, readoutW, readoutH))
	fillRect(img, 0, 0, readoutW, readoutH, color.RGBA{12, 16, 24, 215})
	fillRect(img, 0, 0, readoutW, 5, color.RGBA{80, 200, 255, 255})

	white := color.RGBA{235, 240, 245, 255}
	dim := color.RGBA{150, 165, 180, 255}
	accent := color.RGBA{120, 220, 255, 255}

	drawText(img, 20, 22, "RECEIVER LINK", accent, 3)
	drawText(img, 20, 78, fmt.Sprintf("Pr    %8.1f dBm", dBm), white, 2)
	drawText(img, 20, 112, fmt.Sprintf("SNR   %8.1f dB", link.SNRdB), white, 2)
	drawText(img, 20, 146, fmt.Sprintf("dist  %8.1f m", link.DistM), dim, 2)
	drawText(img, 20, 180, fmt.Sprintf("freq  %8.0f MHz", freq/1e6), dim, 2)
	drawText(img, 20, 214, fmt.Sprintf("lamda %8.1f m", sim.SpeedOfLight/freq), dim, 2)
	drawText(img, 20, 248, fmt.Sprintf("Ptx   %8.1f dBm", sim.DBm(power)), dim, 2)
	return img
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

// drawConfig renders and blits the right-edge config drawer for the selected
// device (TX or RX), including the OPEN GRAPH button that enters the editor for
// that device's chain.
func drawConfig(c *app.Ctx, hud *HUD, lab *Lab, txd *TxDevice, rxd *RxDevice) {
	if lab.Sel == SelNone {
		return
	}
	var title, l1, l2 string
	switch lab.Sel {
	case SelTx:
		if txd == nil {
			return
		}
		title = "TRANSMITTER"
		l1 = fmt.Sprintf("Power  %7.1f dBm", sim.DBm(txd.PowerW))
		l2 = fmt.Sprintf("Freq   %7.0f MHz", txd.FreqHz/1e6)
	case SelRx:
		if rxd == nil {
			return
		}
		title = "RECEIVER"
		l1 = fmt.Sprintf("Gain   %7.0f dBi", rxd.GainDBi)
		l2 = fmt.Sprintf("NoiseF %7.1f dB", rxd.NoiseFigDB)
	}

	// Full-height drawer flush against the right edge of the screen.
	w, h := windowSize(c)
	key := fmt.Sprintf("%s|%s|%s|%d", title, l1, l2, h)
	if key != hud.lastConfig {
		if err := render3d.LoadPanel(c, "config", buildConfig(title, l1, l2, h)); err == nil {
			hud.lastConfig = key
		}
	}
	render3d.DrawPanel(c, "config", w-configW, 0, configW, h)
}

func buildConfig(title, l1, l2 string, configH int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, configW, configH))
	fillRect(img, 0, 0, configW, configH, color.RGBA{16, 14, 22, 224})
	// Vertical accent stripe on the inner (left) edge — the "drawer" lip.
	fillRect(img, 0, 0, 6, configH, color.RGBA{255, 200, 90, 255})

	white := color.RGBA{240, 238, 245, 255}
	dim := color.RGBA{170, 160, 185, 255}
	accent := color.RGBA{255, 210, 120, 255}

	drawText(img, 28, 28, title, accent, 3)

	drawText(img, 28, 96, l1, white, 3)
	drawText(img, 28, 138, "up / down", dim, 2)

	drawText(img, 28, 190, l2, white, 3)
	drawText(img, 28, 232, "left / right", dim, 2)

	// OPEN GRAPH button — local rect matches openGraphButtonRect's drawer-local
	// position (x − (fbW−configW)).
	drawButton(img, rect{24, 300, configW - 48, 56}, "OPEN GRAPH", color.RGBA{40, 70, 110, 255})

	drawText(img, 28, configH-34, "drag to move", dim, 2)
	return img
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
