package main

import (
	"math/rand"

	"github.com/hvuhsg/spliti/app"
)

// Over-the-air scene: the channel adds noise to the signal. The transmitted point
// sits at its ideal spot, but each received sample lands a little off — a fuzzy
// cloud whose spread grows with the noise level. On the wave, the same noise
// shows up as fuzz on top of the clean carrier. Crank the noise until the cloud
// starts spilling across the axes (into the wrong quadrant) — that is a bit error
// waiting to happen, which the next two stages deal with.

const (
	chCloud = 0 // a fresh cloud of received samples each frame (the noise distribution)
	chSent  = 1 // the ideal transmitted point
	chRx    = 2 // the current (held) received sample
	chClean = 3 // clean transmitted wave
	chRecv  = 4 // received wave (clean + fuzz)

	chCloudN = 48
	chWaveN  = 110
)

var (
	chPlaneVP   = viewport{X: 8, Y: 22, W: 54, H: 54}
	chPlaneData = dataRect{X0: -1.7, X1: 1.7, Y0: -1.7, Y1: 1.7}
	chWaveVP    = viewport{X: 70, Y: 24, W: 84, H: 22}
	chWaveData  = dataRect{X0: 0, X1: 1, Y0: -1.4, Y1: 1.4}
)

func setupChannel(c *app.Ctx) {
	cmds := c.Commands()
	ui := app.GetResource[uiState](c)
	ui.lastSym, ui.lastNoiseStep = -1, -1

	spawnBackButton(c)
	spawnLabel(c, "ch:title", "OVER THE AIR", worldW/2, 8, 5, colText, zLabel)

	setupIQPlane(c, chPlaneVP, chPlaneData, "ch", false)
	spawnSeries(cmds, chCloud, chCloudN, 1.1, colAccent)
	spawnSeries(cmds, chSent, 1, 4.2, colHi)
	spawnSeries(cmds, chRx, 1, 3.0, colSum)
	spawnLabel(c, "ch:plab", "ideal point (white) + received samples (blue)", chPlaneVP.X+chPlaneVP.W/2, chPlaneVP.Y+chPlaneVP.H+5, 2.6, colMuted, zLabel)

	// Wave panel: clean vs received.
	spawnSprite(cmds, "sq", chWaveVP.X-2, chWaveVP.Y-2, chWaveVP.W+4, chWaveVP.H+4, colPanel, zPanel)
	_, zy := chWaveVP.mapPoint(chWaveData, 0, 0)
	spawnSprite(cmds, "sq", chWaveVP.X, zy-0.12, chWaveVP.W, 0.24, colAxis, zAxis)
	spawnSeries(cmds, chClean, chWaveN, 1.0, colMuted)
	spawnSeries(cmds, chRecv, chWaveN, 1.4, colSum)
	spawnLabelLeft(c, "ch:wl", "clean wave (grey)  vs  received wave (amber)", chWaveVP.X, chWaveVP.Y-3, 2.8, colMuted, zLabel)

	// Right column: noise control + explanation.
	const rx = 70.0
	spawnLabelLeft(c, "ch:noise", noiseBar(ui.noiseStep), rx, 54, 4, colSum, zLabel)
	spawnLabelLeft(c, "ch:e1", "noise nudges every received", rx, 62, 3, colText, zLabel)
	spawnLabelLeft(c, "ch:e2", "sample off its ideal spot.", rx, 67, 3, colText, zLabel)
	spawnLabelLeft(c, "ch:e3", "too much, and a sample drifts", rx, 73, 3, colText, zLabel)
	spawnLabelLeft(c, "ch:e4", "into the wrong quadrant = error.", rx, 78, 3, colText, zLabel)

	spawnLabel(c, "ch:hint", "Left/Right: symbol      Up/Down: noise", worldW/2, 88, 2.8, colMuted, zLabel)
}

func channelInput(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	handleRxKeys(c, ui)
	if dirty(ui) {
		loadLabel(c, "ch:noise", noiseBar(ui.noiseStep))
		clean(ui)
	}
}

func channelDraw(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	now := elapsed(c)
	resample(ui, now)

	b0, b1 := bitsOf(ui.sym)
	s := qpskMap(b0, b1)
	sig := sigmaOf(ui.noiseStep)

	placeSeries(c, chSent, func(int) (float32, float32) {
		return chPlaneVP.mapPoint(chPlaneData, s.I, s.Q)
	})
	placeSeries(c, chRx, func(int) (float32, float32) {
		return chPlaneVP.mapPoint(chPlaneData, ui.rxI, ui.rxQ)
	})
	// A new random cloud each frame visualizes the noise distribution as a haze.
	placeSeries(c, chCloud, func(int) (float32, float32) {
		return chPlaneVP.mapPoint(chPlaneData, s.I+sig*rand.NormFloat64(), s.Q+sig*rand.NormFloat64())
	})

	// Waves: clean carrier for the ideal symbol vs the received vector + fuzz.
	rxSym := Symbol{I: ui.rxI, Q: ui.rxQ}
	t0 := now * 0.3
	placeSeries(c, chClean, func(i int) (float32, float32) {
		u := float64(i) / float64(chWaveN-1)
		v := passband(s, rxFreq, u+t0)
		return chWaveVP.mapPoint(chWaveData, u, clampf(v, -1.39, 1.39))
	})
	placeSeries(c, chRecv, func(i int) (float32, float32) {
		u := float64(i) / float64(chWaveN-1)
		v := passband(rxSym, rxFreq, u+t0) + sig*0.7*rand.NormFloat64()
		return chWaveVP.mapPoint(chWaveData, u, clampf(v, -1.39, 1.39))
	})
}
