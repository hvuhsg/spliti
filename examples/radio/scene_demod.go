package main

import (
	"fmt"
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
)

// Wave -> symbols scene: how the receiver gets the I/Q point back out of the
// wave. It multiplies the received wave by a cosine and by a sine carrier and
// averages each product over one symbol — coherent demodulation. The average of
// (wave x cos) is I/2, and of (wave x -sin) is Q/2, so x2 gives the recovered
// point. The shaded product curves spend more time positive than negative (or
// vice-versa); that imbalance IS the recovered coordinate.

const (
	dmWave  = 0 // received wave r(t)
	dmProdC = 1 // r(t) * cos
	dmProdS = 2 // r(t) * (-sin)
	dmRec   = 3 // recovered point on the I/Q plane

	dmN = 150
)

var (
	dmWaveVP  = viewport{X: 6, Y: 17, W: 82, H: 15}
	dmCVP     = viewport{X: 6, Y: 39, W: 82, H: 15}
	dmSVP     = viewport{X: 6, Y: 61, W: 82, H: 15}
	dmData    = dataRect{X0: 0, X1: 1, Y0: -1.4, Y1: 1.4}
	dmPlaneVP = viewport{X: 98, Y: 26, W: 52, H: 52}
	dmPlaneDt = dataRect{X0: -1.6, X1: 1.6, Y0: -1.6, Y1: 1.6}
)

func setupDemod(c *app.Ctx) {
	cmds := c.Commands()
	ui := app.GetResource[uiState](c)
	ui.lastSym, ui.lastNoiseStep = -1, -1

	spawnBackButton(c)
	spawnLabel(c, "dm:title", "WAVE  ->  SYMBOLS", worldW/2, 8, 5, colText, zLabel)

	demodPanel(c, "dm:lw", dmWaveVP, "received wave  r(t)", colSum)
	demodPanel(c, "dm:lc", dmCVP, "r(t) x cos     ->  average x2 = I", colI)
	demodPanel(c, "dm:ls", dmSVP, "r(t) x (-sin)  ->  average x2 = Q", colQ)
	spawnSeries(cmds, dmWave, dmN, 1.3, colSum)
	spawnSeries(cmds, dmProdC, dmN, 1.2, colI)
	spawnSeries(cmds, dmProdS, dmN, 1.2, colQ)

	setupIQPlane(c, dmPlaneVP, dmPlaneDt, "dm", false)
	spawnSeries(cmds, dmRec, 1, 3.4, colSum)
	spawnLabel(c, "dm:plab", "recovered point", dmPlaneVP.X+dmPlaneVP.W/2, dmPlaneVP.Y+dmPlaneVP.H+4, 2.8, colMuted, zLabel)
	spawnLabelLeft(c, "dm:I", "I = +0.71", dmPlaneVP.X, dmPlaneVP.Y+dmPlaneVP.H+9, 3.2, colI, zLabel)
	spawnLabelLeft(c, "dm:Q", "Q = +0.71", dmPlaneVP.X+26, dmPlaneVP.Y+dmPlaneVP.H+9, 3.2, colQ, zLabel)

	spawnLabel(c, "dm:hint", "Left/Right: symbol      Up/Down: noise", worldW/2, 88, 2.8, colMuted, zLabel)
}

// demodPanel draws a labelled wave panel with a zero axis.
func demodPanel(c *app.Ctx, ref string, vp viewport, label string, col webgpu.Color) {
	cmds := c.Commands()
	spawnSprite(cmds, "sq", vp.X-2, vp.Y-2, vp.W+4, vp.H+4, colPanel, zPanel)
	_, zy := vp.mapPoint(dmData, 0, 0)
	spawnSprite(cmds, "sq", vp.X, zy-0.1, vp.W, 0.2, colAxis, zAxis)
	spawnLabelLeft(c, ref, label, vp.X+1, vp.Y-2.5, 2.7, col, zLabel)
}

func demodInput(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	handleRxKeys(c, ui)
}

func demodDraw(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	now := elapsed(c)
	newSample := resample(ui, now)

	// The received wave carries the noisy received vector rx; demodulating it
	// (multiply + average) returns rx exactly, which is what the readout shows.
	rx := Symbol{I: ui.rxI, Q: ui.rxQ}
	w := 2 * math.Pi * rxFreq
	r := func(u float64) float64 { return passband(rx, rxFreq, u) }

	placeSeries(c, dmWave, func(i int) (float32, float32) {
		u := float64(i) / float64(dmN-1)
		return dmWaveVP.mapPoint(dmData, u, clampf(r(u), -1.39, 1.39))
	})
	placeSeries(c, dmProdC, func(i int) (float32, float32) {
		u := float64(i) / float64(dmN-1)
		return dmCVP.mapPoint(dmData, u, clampf(r(u)*math.Cos(w*u), -1.39, 1.39))
	})
	placeSeries(c, dmProdS, func(i int) (float32, float32) {
		u := float64(i) / float64(dmN-1)
		return dmSVP.mapPoint(dmData, u, clampf(r(u)*-math.Sin(w*u), -1.39, 1.39))
	})

	placeSeries(c, dmRec, func(int) (float32, float32) {
		return dmPlaneVP.mapPoint(dmPlaneDt, ui.rxI, ui.rxQ)
	})

	if newSample {
		loadLabel(c, "dm:I", fmt.Sprintf("I = %+.2f", ui.rxI))
		loadLabel(c, "dm:Q", fmt.Sprintf("Q = %+.2f", ui.rxQ))
	}
}
