package main

import (
	"fmt"
	"math"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
)

// Constellation scene: how 2 bits become one complex symbol (a point on the I/Q
// plane). The selected symbol is shown as a phasor (vector from the origin), and
// a live readout gives its I, Q and phase.

const (
	cPoints = 0 // the four QPSK constellation points
	cPhasor = 1 // the vector from origin to the selected point
)

var (
	constVP   = viewport{X: 22, Y: 24, W: 56, H: 56}
	constData = dataRect{X0: -1.4, X1: 1.4, Y0: -1.4, Y1: 1.4}
)

// constPoint returns the world position of QPSK point idx.
func constPoint(idx int) (float32, float32) {
	b0, b1 := bitsOf(idx)
	s := qpskMap(b0, b1)
	return constVP.mapPoint(constData, s.I, s.Q)
}

func setupConstellation(c *app.Ctx) {
	cmds := c.Commands()
	ui := app.GetResource[uiState](c)
	ui.lastSym = -1 // force the readout to refresh on the first frame

	spawnBackButton(c)
	spawnLabel(c, "co:title", "BITS  ->  SYMBOLS  (QPSK)", worldW/2, 9, 5, colText, zLabel)

	// Plot panel background.
	spawnSprite(cmds, "sq", constVP.X-4, constVP.Y-4, constVP.W+8, constVP.H+8, colPanel, zPanel)

	// I (horizontal) and Q (vertical) axes through the centre.
	cx0, cy0 := constVP.mapPoint(constData, 0, 0)
	spawnSprite(cmds, "sq", constVP.X, cy0-0.15, constVP.W, 0.3, colAxis, zAxis)
	spawnSprite(cmds, "sq", cx0-0.15, constVP.Y, 0.3, constVP.H, colAxis, zAxis)
	spawnLabel(c, "co:iax", "I", constVP.X+constVP.W+2.5, cy0, 3.2, colMuted, zLabel)
	spawnLabel(c, "co:qax", "Q", cx0, constVP.Y-3, 3.2, colMuted, zLabel)

	// Unit circle (|symbol| = 1). The unit radius in world units:
	ux, _ := constVP.mapPoint(constData, 1, 0)
	r := ux - cx0
	spawnSprite(cmds, "ring", cx0-r, cy0-r, 2*r, 2*r, colGrid, zRing)

	// The four points + their bit labels.
	spawnSeries(cmds, cPoints, 4, 4.5, colAccent)
	for idx := 0; idx < 4; idx++ {
		px, py := constPoint(idx)
		b0, b1 := bitsOf(idx)
		spawnLabel(c, "co:pt"+itoa(idx), fmt.Sprintf("%d%d", b0, b1), px, py-4.5, 3, colText, zLabel)
	}

	// Phasor (origin -> selected point), drawn as a line of small dots.
	spawnSeries(cmds, cPhasor, 16, 1.6, colSum)

	// Right column: explanation + live readout.
	const rx = 92.0
	spawnLabelLeft(c, "co:h1", "A COMPLEX NUMBER", rx, 22, 4, colAccent, zLabel)
	spawnLabelLeft(c, "co:h2", "2 bits pick 1 of 4 points", rx, 29, 3, colText, zLabel)
	spawnLabelLeft(c, "co:h3", "bit 0 -> I sign:  0=+   1=-", rx, 34, 3, colMuted, zLabel)
	spawnLabelLeft(c, "co:h4", "bit 1 -> Q sign:  0=+   1=-", rx, 39, 3, colMuted, zLabel)

	spawnSprite(cmds, "sq", rx-2, 45, 60, 28, colPanel, zPanel)
	spawnLabelLeft(c, "co:bits", "bits  = 00", rx, 50, 3.4, colSum, zLabel)
	spawnLabelLeft(c, "co:i", "I     = +0.71", rx, 56, 3.4, colI, zLabel)
	spawnLabelLeft(c, "co:q", "Q     = +0.71", rx, 62, 3.4, colQ, zLabel)
	spawnLabelLeft(c, "co:ph", "angle = 045 deg", rx, 68, 3.4, colText, zLabel)

	spawnLabel(c, "co:hint", "Left / Right (or Up / Down): change the 2 bits", worldW/2, 84, 3, colMuted, zLabel)
}

// constellationInput cycles the selected symbol and refreshes the readout when
// it changes.
func constellationInput(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	for _, ev := range app.ReadEvents[webgpu.KeyEvent](c) {
		if ev.Action != glfw.Press && ev.Action != glfw.Repeat {
			continue
		}
		switch ev.Key {
		case glfw.KeyRight, glfw.KeyUp:
			ui.sym = (ui.sym + 1) & 3
		case glfw.KeyLeft, glfw.KeyDown:
			ui.sym = (ui.sym + 3) & 3
		}
	}
	if ui.sym != ui.lastSym {
		updateConstellationReadout(c, ui.sym)
		ui.lastSym = ui.sym
	}
}

// updateConstellationReadout re-bakes the readout labels. The strings keep a
// fixed width, so replacing the texture preserves each sprite's geometry.
func updateConstellationReadout(c *app.Ctx, sym int) {
	b0, b1 := bitsOf(sym)
	s := qpskMap(b0, b1)
	deg := int(math.Round(math.Atan2(s.Q, s.I) * 180 / math.Pi))
	if deg < 0 {
		deg += 360
	}
	loadLabel(c, "co:bits", fmt.Sprintf("bits  = %d%d", b0, b1))
	loadLabel(c, "co:i", fmt.Sprintf("I     = %+.2f", s.I))
	loadLabel(c, "co:q", fmt.Sprintf("Q     = %+.2f", s.Q))
	loadLabel(c, "co:ph", fmt.Sprintf("angle = %03d deg", deg))
}

// constellationDraw positions the points and phasor and highlights the selection.
func constellationDraw(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	placeSeries(c, cPoints, constPoint)
	recolorSeries(c, cPoints, func(idx int) webgpu.Color {
		if idx == ui.sym {
			return colHi
		}
		return colMuted
	})

	b0, b1 := bitsOf(ui.sym)
	sym := qpskMap(b0, b1)
	placeSeries(c, cPhasor, func(i int) (float32, float32) {
		f := float64(i) / 15.0
		return constVP.mapPoint(constData, sym.I*f, sym.Q*f)
	})
}
