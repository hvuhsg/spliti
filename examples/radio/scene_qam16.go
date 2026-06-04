package main

import (
	"fmt"
	"math"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
)

// 16-QAM scene: a denser modulation. Instead of QPSK's 4 points, 16-QAM has 16,
// so each symbol carries 4 bits. Two bits set the I level and two set the Q
// level (each from -3,-1,+1,+3, Gray coded). The big new idea vs QPSK: points sit
// at different distances from the origin, so amplitude AND phase both carry data
// — but the points are closer together, so it needs a cleaner channel.

const (
	qmPoints = 0 // the 16 constellation points
	qmPhasor = 1 // origin -> selected point
	qmWave   = 2 // the selected symbol's wave (amplitude varies!)

	qmWaveN = 130
)

var (
	qmVP     = viewport{X: 10, Y: 20, W: 58, H: 58}
	qmData   = dataRect{X0: -1.25, X1: 1.25, Y0: -1.25, Y1: 1.25}
	qmWaveVP = viewport{X: 80, Y: 64, W: 72, H: 16}
	qmWaveDt = dataRect{X0: 0, X1: 1, Y0: -1.5, Y1: 1.5}
)

// qamPrev tracks the last grid cell the readouts were baked for (-1 forces a bake).
var qamPrev int

// qamBits returns the 4 bits of the current selection: I Gray pair then Q Gray pair.
func qamBits(ui *uiState) (ihi, ilo, qhi, qlo int) {
	gi, gq := gray2(ui.qamI), gray2(ui.qamQ)
	return (gi >> 1) & 1, gi & 1, (gq >> 1) & 1, gq & 1
}

func setupQAM16(c *app.Ctx) {
	cmds := c.Commands()
	qamPrev = -1

	spawnBackButton(c)
	spawnLabel(c, "qm:title", "16-QAM CONSTELLATION", worldW/2, 8, 5, colText, zLabel)

	// Plane: background, axes, the 16 points, and their 4-bit labels.
	spawnSprite(cmds, "sq", qmVP.X-4, qmVP.Y-4, qmVP.W+8, qmVP.H+8, colPanel, zPanel)
	cx, cy := qmVP.mapPoint(qmData, 0, 0)
	spawnSprite(cmds, "sq", qmVP.X, cy-0.12, qmVP.W, 0.24, colAxis, zAxis)
	spawnSprite(cmds, "sq", cx-0.12, qmVP.Y, 0.24, qmVP.H, colAxis, zAxis)
	spawnLabel(c, "qm:iax", "I", qmVP.X+qmVP.W+3, cy, 3, colMuted, zLabel)
	spawnLabel(c, "qm:qax", "Q", cx, qmVP.Y-3, 3, colMuted, zLabel)

	spawnSeries(cmds, qmPhasor, 16, 1.3, colSum)
	spawnSeries(cmds, qmPoints, 16, 3.4, colAccent)
	for idx := 0; idx < 16; idx++ {
		i, q := idx/4, idx%4
		p := qam16Point(i, q)
		px, py := qmVP.mapPoint(qmData, p.I, p.Q)
		gi, gq := gray2(i), gray2(q)
		spawnLabel(c, "qm:l"+itoa(idx), fmt.Sprintf("%d%d%d%d", (gi>>1)&1, gi&1, (gq>>1)&1, gq&1),
			px, py-3.4, 2.1, colMuted, zLabel)
	}

	// Right column: readout + contrast with QPSK.
	const rx = 80.0
	spawnLabelLeft(c, "qm:h1", "4 BITS PER SYMBOL", rx, 19, 4.4, colI, zLabel)
	spawnLabelLeft(c, "qm:h2", "QPSK had 4 points (2 bits).", rx, 26, 3, colText, zLabel)
	spawnLabelLeft(c, "qm:h3", "16-QAM has 16 points (4 bits)", rx, 31, 3, colText, zLabel)
	spawnLabelLeft(c, "qm:h4", "- double the data per symbol.", rx, 36, 3, colText, zLabel)

	spawnSprite(cmds, "sq", rx-2, 41, 74, 18, colPanel, zPanel)
	spawnLabelLeft(c, "qm:bits", "bits      = 0000", rx, 46, 3.2, colSum, zLabel)
	spawnLabelLeft(c, "qm:iq", "I = +0.32  Q = +0.95", rx, 51, 3.2, colText, zLabel)
	spawnLabelLeft(c, "qm:ap", "amplitude = 1.00   phase = 045", rx, 56, 3.0, colAccent, zLabel)

	demodPanel2(c, "qm:wl", qmWaveVP, "this symbol's wave (amplitude varies!)")
	spawnSeries(cmds, qmWave, qmWaveN, 1.4, colSum)

	spawnLabel(c, "qm:hint", "Arrows: move the point around the 4x4 grid", worldW/2, 88, 2.8, colMuted, zLabel)
}

// demodPanel2 draws a small wave panel (separate from demodPanel to avoid that
// scene's dmData coupling).
func demodPanel2(c *app.Ctx, ref string, vp viewport, label string) {
	cmds := c.Commands()
	spawnSprite(cmds, "sq", vp.X-2, vp.Y-2, vp.W+4, vp.H+4, colPanel, zPanel)
	_, zy := vp.mapPoint(qmWaveDt, 0, 0)
	spawnSprite(cmds, "sq", vp.X, zy-0.1, vp.W, 0.2, colAxis, zAxis)
	spawnLabelLeft(c, ref, label, vp.X, vp.Y-2.6, 2.6, colMuted, zLabel)
}

func qam16Input(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	for _, ev := range app.ReadEvents[webgpu.KeyEvent](c) {
		if ev.Action != glfw.Press && ev.Action != glfw.Repeat {
			continue
		}
		switch ev.Key {
		case glfw.KeyLeft:
			ui.qamI = max(0, ui.qamI-1)
		case glfw.KeyRight:
			ui.qamI = min(3, ui.qamI+1)
		case glfw.KeyUp:
			ui.qamQ = min(3, ui.qamQ+1)
		case glfw.KeyDown:
			ui.qamQ = max(0, ui.qamQ-1)
		}
	}
}

func qam16Draw(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	sel := qam16Point(ui.qamI, ui.qamQ)
	cur := ui.qamI*4 + ui.qamQ

	// Position the 16 points and highlight the selected one.
	placeSeries(c, qmPoints, func(idx int) (float32, float32) {
		p := qam16Point(idx/4, idx%4)
		return qmVP.mapPoint(qmData, p.I, p.Q)
	})
	recolorSeries(c, qmPoints, func(idx int) webgpu.Color {
		if idx == cur {
			return colHi
		}
		return colAccent
	})

	// Phasor from origin to the selected point (shows amplitude + phase).
	placeSeries(c, qmPhasor, func(i int) (float32, float32) {
		f := float64(i) / 15.0
		return qmVP.mapPoint(qmData, sel.I*f, sel.Q*f)
	})

	// The selected symbol's wave, animated; its amplitude depends on the point.
	t0 := elapsed(c) * 0.3
	placeSeries(c, qmWave, func(i int) (float32, float32) {
		u := float64(i) / float64(qmWaveN-1)
		v := passband(sel, 3, u+t0)
		return qmWaveVP.mapPoint(qmWaveDt, u, clampf(v, -1.49, 1.49))
	})

	if cur != qamPrev {
		qamPrev = cur
		ihi, ilo, qhi, qlo := qamBits(ui)
		amp := math.Hypot(sel.I, sel.Q)
		loadLabel(c, "qm:bits", fmt.Sprintf("bits      = %d%d%d%d", ihi, ilo, qhi, qlo))
		loadLabel(c, "qm:iq", fmt.Sprintf("I = %+.2f  Q = %+.2f", sel.I, sel.Q))
		loadLabel(c, "qm:ap", fmt.Sprintf("amplitude = %.2f   phase = %03d", amp, phaseDeg(sel)))
	}
}
