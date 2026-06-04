package main

import (
	"fmt"
	"math"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
)

// I/Q wave scene: how a constellation point becomes a wave. The point is spun at
// the carrier frequency; the transmitted wave is the moving shadow of the
// spinning vector — its sideways position, s(t) = Re{c·e^{j2πft}} = I·cos − Q·sin
// = A·cos(2πft+φ). The wave scrolls downward out of the circle so the spinning
// tip and the newest wave sample line up under a clean vertical connector.

const (
	pVec  = 0 // the spinning vector (origin -> rotating tip)
	pWave = 1 // the unrolling wave, flowing downward
	pConn = 2 // vertical connector tip -> wave head
	pTip  = 3 // the bright rotating tip
	pSym  = 4 // the fixed symbol point on the circle

	vecN  = 16
	waveN = 170
	connN = 18

	// circle geometry (world units)
	cxC = 42.0
	cyC = 28.0
	rC  = 17.0

	// wave band below the circle
	waveTopY = 47.0
	waveBotY = 86.0

	revsPerSec  = 0.22 // visual spin rate (the real carrier is far faster)
	cyclesShown = 2.6  // wave cycles visible down the band
)

func setupIQWave(c *app.Ctx) {
	cmds := c.Commands()
	ui := app.GetResource[uiState](c)
	ui.lastSym = -1

	spawnBackButton(c)
	spawnLabel(c, "iq:title", "SYMBOLS  ->  WAVE", worldW/2, 8, 5, colText, zLabel)

	// Circle (|symbol| = 1) and its I/Q axes.
	spawnSprite(cmds, "ring", cxC-rC, cyC-rC, 2*rC, 2*rC, colGrid, zRing)
	spawnSprite(cmds, "sq", cxC-rC-3, cyC-0.12, 2*rC+6, 0.24, colAxis, zAxis) // I axis
	spawnSprite(cmds, "sq", cxC-0.12, cyC-rC-3, 0.24, 2*rC+6, colAxis, zAxis) // Q axis
	spawnLabel(c, "iq:Iax", "I", cxC+rC+4, cyC, 3, colMuted, zLabel)
	spawnLabel(c, "iq:Qax", "Q", cxC, cyC-rC-4, 3, colMuted, zLabel)

	// Spinning vector, fixed symbol point, bright tip, connector, and the wave.
	spawnSeries(cmds, pVec, vecN, 1.3, colAccent)
	spawnSeries(cmds, pSym, 1, 4.0, colHi)
	spawnSeries(cmds, pTip, 1, 3.6, colSum)
	spawnSeries(cmds, pConn, connN, 0.9, colMuted)
	spawnSeries(cmds, pWave, waveN, 1.7, colSum)

	// "time" arrow next to the wave band.
	spawnLabel(c, "iq:tarrow", "time", cxC-rC-7, (waveTopY+waveBotY)/2, 2.8, colMuted, zLabel)

	// Right column: the explanation and live readout.
	const rx = 70.0
	spawnLabelLeft(c, "iq:h1", "SPIN THE POINT", rx, 18, 4.4, colAccent, zLabel)
	spawnLabelLeft(c, "iq:h2", "spin the symbol at the carrier", rx, 25, 3, colText, zLabel)
	spawnLabelLeft(c, "iq:h3", "frequency; the wave is its", rx, 30, 3, colText, zLabel)
	spawnLabelLeft(c, "iq:h4", "moving shadow (sideways position)", rx, 35, 3, colText, zLabel)

	spawnSprite(cmds, "sq", rx-2, 41, 86, 26, colPanel, zPanel)
	spawnLabelLeft(c, "iq:rA", "amplitude = 1.00  (= distance)", rx, 46, 3.2, colSum, zLabel)
	spawnLabelLeft(c, "iq:rP", "phase     = 045 deg (= angle)", rx, 52, 3.2, colSum, zLabel)
	spawnLabelLeft(c, "iq:rI", "I = +0.71   (sideways part)", rx, 58, 3.2, colI, zLabel)
	spawnLabelLeft(c, "iq:rQ", "Q = +0.71   (up/down part)", rx, 64, 3.2, colQ, zLabel)

	spawnLabelLeft(c, "iq:n1", "s(t) = I.cos - Q.sin = cos(2.pi.f.t + phase)", rx, 73, 2.8, colMuted, zLabel)
	spawnLabelLeft(c, "iq:n2", "QPSK: all 4 points share one amplitude -", rx, 78, 2.8, colMuted, zLabel)
	spawnLabelLeft(c, "iq:n3", "only the phase (angle) changes.", rx, 82, 2.8, colMuted, zLabel)

	spawnLabel(c, "iq:hint", "Left / Right: change the symbol", worldW/2, 88, 2.8, colMuted, zLabel)
}

func iqwaveInput(c *app.Ctx) {
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
		b0, b1 := bitsOf(ui.sym)
		s := qpskMap(b0, b1)
		deg := int(math.Round(math.Atan2(s.Q, s.I) * 180 / math.Pi))
		if deg < 0 {
			deg += 360
		}
		loadLabel(c, "iq:rP", fmt.Sprintf("phase     = %03d deg (= angle)", deg))
		loadLabel(c, "iq:rI", fmt.Sprintf("I = %+.2f   (sideways part)", s.I))
		loadLabel(c, "iq:rQ", fmt.Sprintf("Q = %+.2f   (up/down part)", s.Q))
		ui.lastSym = ui.sym
	}
}

func iqwaveDraw(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	b0, b1 := bitsOf(ui.sym)
	sym := qpskMap(b0, b1)
	phi := math.Atan2(sym.Q, sym.I) // the point's angle = the wave's phase
	w := 2 * math.Pi * revsPerSec
	now := elapsed(c)
	theta := phi + w*now // current angle of the spinning vector

	// real (sideways) and screen-vertical offset of a unit vector at angle a.
	tipX := func(a float64) float32 { return cxC + rC*float32(math.Cos(a)) }
	tipY := func(a float64) float32 { return cyC - rC*float32(math.Sin(a)) } // screen Y is down

	// Fixed symbol point on the circle (where the spinning starts).
	placeSeries(c, pSym, func(int) (float32, float32) { return tipX(phi), tipY(phi) })

	// Spinning vector from centre to the rotating tip.
	placeSeries(c, pVec, func(i int) (float32, float32) {
		f := float64(i) / float64(vecN-1)
		return cxC + float32(f)*(tipX(theta)-cxC), cyC + float32(f)*(tipY(theta)-cyC)
	})
	placeSeries(c, pTip, func(int) (float32, float32) { return tipX(theta), tipY(theta) })

	// Wave scrolls downward: top sample is "now" and equals the tip's sideways
	// position s(t)=cos(theta); deeper rows are older. Amplitude is horizontal,
	// centred under the circle, so the connector to the tip is vertical.
	tSpan := cyclesShown / revsPerSec // seconds of history shown down the band
	waveX := func(t float64) float32 { return cxC + rC*float32(math.Cos(phi+w*t)) }
	placeSeries(c, pWave, func(i int) (float32, float32) {
		u := float64(i) / float64(waveN-1) // 0 at top (now) .. 1 at bottom (oldest)
		y := waveTopY + float32(u)*(waveBotY-waveTopY)
		return waveX(now - u*tSpan), y
	})

	// Vertical connector from the tip down to the head of the wave.
	headX := waveX(now)
	placeSeries(c, pConn, func(i int) (float32, float32) {
		f := float64(i) / float64(connN-1)
		return headX, tipY(theta) + float32(f)*(waveTopY-tipY(theta))
	})
}
