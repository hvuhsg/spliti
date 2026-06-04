package main

import (
	"fmt"
	"math/rand"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
)

// Shared model for the receive-side scenes (Channel, Demod, Decode). They all
// view the same picture: the transmitted point gets a noise vector added to it,
// producing a received vector rx = (rxI, rxQ). Channel shows the noise happening,
// Demod recovers rx from the wave, Decode decides bits from rx. Holding rx in the
// shared uiState keeps the three scenes telling one continuous story.

const (
	maxNoiseStep   = 6
	sampleInterval = 0.45 // seconds a received sample is held before re-drawing a new one
	rxFreq         = 3.0  // carrier cycles shown across a wave window
)

// sigmaOf maps a noise step to the per-axis Gaussian standard deviation.
func sigmaOf(step int) float64 { return float64(step) * 0.09 }

// handleRxKeys applies the shared controls: Left/Right pick the symbol, Up/Down
// change the noise level. Any change forces an immediate new received sample.
func handleRxKeys(c *app.Ctx, ui *uiState) {
	for _, ev := range app.ReadEvents[webgpu.KeyEvent](c) {
		if ev.Action != glfw.Press && ev.Action != glfw.Repeat {
			continue
		}
		switch ev.Key {
		case glfw.KeyRight:
			ui.sym = (ui.sym + 1) & 3
			ui.nextSample = 0
		case glfw.KeyLeft:
			ui.sym = (ui.sym + 3) & 3
			ui.nextSample = 0
		case glfw.KeyUp:
			if ui.noiseStep < maxNoiseStep {
				ui.noiseStep++
			}
			ui.nextSample = 0
		case glfw.KeyDown:
			if ui.noiseStep > 0 {
				ui.noiseStep--
			}
			ui.nextSample = 0
		}
	}
}

// resample draws a fresh received vector when the hold time elapses. Returns true
// on the frames where a new sample was produced (so a scene can re-bake readouts
// and tally errors only then, not every frame).
func resample(ui *uiState, now float64) bool {
	if now < ui.nextSample {
		return false
	}
	b0, b1 := bitsOf(ui.sym)
	s := qpskMap(b0, b1)
	sig := sigmaOf(ui.noiseStep)
	ui.rxI = s.I + sig*rand.NormFloat64()
	ui.rxQ = s.Q + sig*rand.NormFloat64()
	ui.nextSample = now + sampleInterval
	return true
}

// dirty reports whether the symbol or noise level changed since the last re-bake.
func dirty(ui *uiState) bool { return ui.sym != ui.lastSym || ui.noiseStep != ui.lastNoiseStep }

// clean records the current symbol/noise as the last re-baked state.
func clean(ui *uiState) { ui.lastSym = ui.sym; ui.lastNoiseStep = ui.noiseStep }

// noiseBar renders a fixed-width "[###---]" gauge string for a noise step.
func noiseBar(step int) string {
	b := make([]byte, maxNoiseStep)
	for i := range b {
		if i < step {
			b[i] = '#'
		} else {
			b[i] = '-'
		}
	}
	return "noise [" + string(b) + "]"
}

func signf(v float64) float32 {
	if v < 0 {
		return -1
	}
	return 1
}

func clampf(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// setupIQPlane draws the static parts of an I/Q constellation plane into vp: the
// background, optional quadrant decision-region tints, the I/Q axes, the unit
// circle, and the four faint ideal points with bit labels. Moving points (the
// received vector etc.) are added by each scene as dot series.
func setupIQPlane(c *app.Ctx, vp viewport, data dataRect, prefix string, regions bool) {
	cmds := c.Commands()
	spawnSprite(cmds, "sq", vp.X-3, vp.Y-3, vp.W+6, vp.H+6, colPanel, zPanel)

	cx, cy := vp.mapPoint(data, 0, 0)
	left, right := vp.X, vp.X+vp.W
	top, bot := vp.Y, vp.Y+vp.H

	if regions {
		tint := func(x, y, w, h float32, col webgpu.Color) {
			col.A = 0.10
			spawnSprite(cmds, "sq", x, y, w, h, col, zPanel+1)
		}
		tint(cx, top, right-cx, cy-top, colAccent) // I+,Q+  -> 00 (top-right)
		tint(cx, cy, right-cx, bot-cy, colQ)       // I+,Q-  -> 01 (bottom-right)
		tint(left, top, cx-left, cy-top, colI)     // I-,Q+  -> 10 (top-left)
		tint(left, cy, cx-left, bot-cy, colSum)    // I-,Q-  -> 11 (bottom-left)
	}

	spawnSprite(cmds, "sq", left, cy-0.12, vp.W, 0.24, colAxis, zAxis)
	spawnSprite(cmds, "sq", cx-0.12, top, 0.24, vp.H, colAxis, zAxis)

	ux, _ := vp.mapPoint(data, 1, 0)
	r := ux - cx
	spawnSprite(cmds, "ring", cx-r, cy-r, 2*r, 2*r, colGrid, zRing)

	for idx := 0; idx < 4; idx++ {
		b0, b1 := bitsOf(idx)
		s := qpskMap(b0, b1)
		px, py := vp.mapPoint(data, s.I, s.Q)
		spawnSprite(cmds, "dot", px-1.5, py-1.5, 3, 3, colMuted, zRing+1)
		spawnLabel(c, prefix+":id"+itoa(idx), fmt.Sprintf("%d%d", b0, b1),
			px+signf(s.I)*5, py-signf(s.Q)*4.5, 2.6, colMuted, zLabel)
	}
}
