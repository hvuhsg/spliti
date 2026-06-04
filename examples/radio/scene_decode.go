package main

import (
	"fmt"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
)

// Symbols -> bits scene: the decision. The plane is split into four decision
// regions (one per symbol). Whichever region the received point falls in is the
// guessed symbol, and that symbol's two bits are the output. Usually the point is
// near its true corner and the bits come back correct; push the noise up and the
// point sometimes lands in the wrong region — a bit error, flashed in red.

const (
	deRx     = 0 // the received point
	deNear   = 1 // the nearest (decided) ideal point
	deArrow  = 2 // line from received point to the decided point
	deStatus = 3 // a coloured dot: green = correct, red = error
	deArrowN = 16

	resWidth = 10 // fixed character width of the result label ("BIT ERROR!")
)

var (
	dePlaneVP = viewport{X: 8, Y: 18, W: 58, H: 58}
	dePlaneDt = dataRect{X0: -1.7, X1: 1.7, Y0: -1.7, Y1: 1.7}
)

func setupDecode(c *app.Ctx) {
	cmds := c.Commands()
	ui := app.GetResource[uiState](c)
	ui.lastSym, ui.lastNoiseStep = -1, -1

	spawnBackButton(c)
	spawnLabel(c, "de:title", "SYMBOLS  ->  BITS", worldW/2, 8, 5, colText, zLabel)

	setupIQPlane(c, dePlaneVP, dePlaneDt, "de", true)
	spawnLabel(c, "de:plab", "tinted = decision regions (one per symbol)", dePlaneVP.X+dePlaneVP.W/2, dePlaneVP.Y+dePlaneVP.H+5, 2.6, colMuted, zLabel)

	spawnSeries(cmds, deArrow, deArrowN, 0.9, colMuted)
	spawnSeries(cmds, deNear, 1, 4.0, colHi)
	spawnSeries(cmds, deRx, 1, 3.2, colSum)
	spawnSeries(cmds, deStatus, 1, 5.0, colI)

	// Right column: the decision spelled out.
	const rx = 76.0
	spawnLabelLeft(c, "de:h", "NEAREST POINT WINS", rx, 20, 4.4, colAccent, zLabel)
	spawnSprite(cmds, "sq", rx-2, 26, 80, 34, colPanel, zPanel)
	spawnLabelLeft(c, "de:sent", "sent     bits = 00", rx, 32, 3.6, colMuted, zLabel)
	spawnLabelLeft(c, "de:dec", "decoded  bits = 00", rx, 40, 3.6, colText, zLabel)
	// A status dot (green=correct, red=error) sits left of the result word. The
	// label is sized for the longest result ("BIT ERROR!") and results are padded
	// to that width, so re-baking a shorter word doesn't squash the texture.
	spawnLabelLeft(c, "de:res", padRight("OK", resWidth), rx+6, 50, 4.4, colText, zLabel)

	spawnLabelLeft(c, "de:e1", "the received point keeps its", rx, 66, 3, colText, zLabel)
	spawnLabelLeft(c, "de:e2", "bits as long as it stays in the", rx, 71, 3, colText, zLabel)
	spawnLabelLeft(c, "de:e3", "right region. raise the noise to", rx, 76, 3, colText, zLabel)
	spawnLabelLeft(c, "de:e4", "make errors happen.", rx, 81, 3, colText, zLabel)

	spawnLabel(c, "de:hint", "Left/Right: symbol      Up/Down: noise", worldW/2, 88, 2.8, colMuted, zLabel)
}

func decodeInput(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	handleRxKeys(c, ui)
}

func decodeDraw(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	now := elapsed(c)
	newSample := resample(ui, now)

	rx := Symbol{I: ui.rxI, Q: ui.rxQ}
	d0, d1 := decide(rx)    // nearest-point decision -> bits
	near := qpskMap(d0, d1) // the decided constellation point
	correct := indexOf(d0, d1) == ui.sym

	placeSeries(c, deRx, func(int) (float32, float32) {
		return dePlaneVP.mapPoint(dePlaneDt, ui.rxI, ui.rxQ)
	})
	placeSeries(c, deNear, func(int) (float32, float32) {
		return dePlaneVP.mapPoint(dePlaneDt, near.I, near.Q)
	})
	placeSeries(c, deArrow, func(i int) (float32, float32) {
		f := float64(i) / float64(deArrowN-1)
		return dePlaneVP.mapPoint(dePlaneDt, ui.rxI+(near.I-ui.rxI)*f, ui.rxQ+(near.Q-ui.rxQ)*f)
	})

	statusCol := colI
	if !correct {
		statusCol = colErr
	}
	placeSeries(c, deStatus, func(int) (float32, float32) { return 77, 50 })
	recolorSeries(c, deStatus, func(int) webgpu.Color { return statusCol })

	if newSample || dirty(ui) {
		b0, b1 := bitsOf(ui.sym)
		loadLabel(c, "de:sent", fmt.Sprintf("sent     bits = %d%d", b0, b1))
		loadLabel(c, "de:dec", fmt.Sprintf("decoded  bits = %d%d", d0, d1))
		if correct {
			loadLabel(c, "de:res", padRight("OK", resWidth))
		} else {
			loadLabel(c, "de:res", padRight("BIT ERROR!", resWidth))
		}
		clean(ui)
	}
}
