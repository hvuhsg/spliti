package main

import (
	"fmt"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
)

// Symbol-time & sampling scene: the two questions that decide a link's timing.
//
//  1. How long is one symbol? A fixed time window holds a chosen number of
//     symbol slots. More slots -> shorter symbol time Ts -> higher symbol rate
//     Rs = 1/Ts -> WIDER bandwidth (B ~ Rs). You can't pack more symbols into the
//     window for free; the spectrum pays for it. That's the Nyquist ceiling seen
//     from the time side.
//  2. How often do we sample each symbol? A symbol is one I/Q point = two numbers
//     (amplitude + phase). One sample per symbol is one equation in two unknowns:
//     phase is lost (underdetermined). Two samples is the minimum that pins both
//     I and Q. More than two is oversampling — redundant, only helps average
//     noise. So fs >= 2*Rs, i.e. >= 2 samples/symbol.
//
// The user drags symbol rate (Left/Right) and samples/symbol (Up/Down) and reads
// Ts, Rs, B, fs and the verdict update live.

const (
	smpWave = 0 // the continuous carrier wave across all symbol slots
	smpSamp = 1 // the sampling-instant dots sitting on the wave
	smpTick = 2 // short ticks on the time axis marking each sample instant
	smpDiv  = 3 // vertical dividers between symbol slots

	smpWaveN   = 480 // dots in the continuous wave
	smpMaxSym  = 8   // most symbol slots the window holds
	smpMaxSps  = 8   // most samples per symbol
	smpMaxSamp = smpMaxSym * smpMaxSps
	smpMaxDiv  = smpMaxSym - 1

	// Non-integer cycles per symbol on purpose: with an integer count, evenly
	// spaced samples would all land at the SAME carrier phase (redundant — the
	// dots would look identical). 1.5 cycles puts the two samples 90 deg apart, so
	// each reads a genuinely different point of the cycle.
	smpCycles   = 1.5 // carrier cycles drawn inside one symbol slot (visual)
	smpWindowMs = 8.0 // the fixed time window the slots share, in ms
)

var (
	smpWaveVP = viewport{X: 8, Y: 22, W: 144, H: 30}
	smpData   = dataRect{X0: 0, X1: 1, Y0: -1.4, Y1: 1.4}
)

// smpSymIdx is the (deterministic) 0..3 symbol index carried by slot s, so the
// slots show a variety of phases. &3 keeps it in range. Shared with the Carrier
// scene so both tell the story with the same data.
func smpSymIdx(s int) int { return (s*3 + 1) & 3 }

// smpSymbol returns the QPSK constellation point for slot s.
func smpSymbol(s int) Symbol { return qpskMap(bitsOf(smpSymIdx(s))) }

// smpDims returns the derived timing numbers for a (nSym, sps) choice.
func smpDims(nSym, sps int) (tsMs float64, rsBd, bHz, fsHz int) {
	tsMs = smpWindowMs / float64(nSym)
	rsBd = int(1000.0/tsMs + 0.5) // 1/ms = 1000/s
	bHz = rsBd                    // minimum (Nyquist) bandwidth ~ symbol rate
	fsHz = rsBd * sps
	return
}

func setupSampling(c *app.Ctx) {
	cmds := c.Commands()
	ui := app.GetResource[uiState](c)
	if ui.smpNSym < 2 || ui.smpNSym > smpMaxSym {
		ui.smpNSym = 4
	}
	if ui.smpSps < 1 || ui.smpSps > smpMaxSps {
		ui.smpSps = 2
	}

	spawnBackButton(c)
	spawnLabel(c, "smp:title", "SYMBOL TIME  &  SAMPLING", worldW/2, 8, 5, colText, zLabel)
	spawnLabel(c, "smp:sub", "how long is one symbol - and how often must we sample it?",
		worldW/2, 14, 2.8, colMuted, zLabel)

	// Wave panel + zero axis.
	spawnSprite(cmds, "sq", smpWaveVP.X-2, smpWaveVP.Y-2, smpWaveVP.W+4, smpWaveVP.H+4, colPanel, zPanel)
	_, zy := smpWaveVP.mapPoint(smpData, 0, 0)
	spawnSprite(cmds, "sq", smpWaveVP.X, zy-0.1, smpWaveVP.W, 0.2, colAxis, zAxis)
	spawnLabelLeft(c, "smp:tlab", "one fixed time window  ->  time", smpWaveVP.X, smpWaveVP.Y-3.5, 2.7, colMuted, zLabel)

	// Slot dividers, the carrier wave, sample ticks on the axis, sample dots.
	spawnMarkers(cmds, smpDiv, smpMaxDiv, "sq", 0.3, smpWaveVP.H, colGrid, zAxis)
	spawnSeries(cmds, smpWave, smpWaveN, 1.0, colSum)
	spawnMarkers(cmds, smpTick, smpMaxSamp, "sq", 0.4, 4, colMuted, zRing)
	spawnSeries(cmds, smpSamp, smpMaxSamp, 2.4, colI)

	// Readout labels. They are spawned at their real (fixed-width) text so the
	// sprite quad is sized correctly; bakeSamplingInfo only swaps the texture for
	// same-length strings, so the geometry never changes.
	ts, rs, b, sp, fs, v := smpStrings(ui)
	// Left column: the symbol-time / bandwidth numbers.
	spawnLabelLeft(c, "smp:rTs", ts, 10, 60, 3.2, colSum, zLabel)
	spawnLabelLeft(c, "smp:rRs", rs, 10, 66, 3.2, colSum, zLabel)
	spawnLabelLeft(c, "smp:rB", b, 10, 72, 3.2, colAccent, zLabel)
	// Right column: the sampling numbers + verdict.
	spawnLabelLeft(c, "smp:rSp", sp, 84, 60, 3.2, colI, zLabel)
	spawnLabelLeft(c, "smp:rFs", fs, 84, 66, 3.2, colI, zLabel)
	spawnLabelLeft(c, "smp:rV", v, 84, 72, 3.0, colMuted, zLabel)

	// Standing principles — why two samples beat one, and the bandwidth ceiling.
	spawnLabel(c, "smp:p0", "each sample is one reading: value = I.cos(phase) - Q.sin(phase)",
		worldW/2, 76, 2.7, colText, zLabel)
	spawnLabel(c, "smp:p1", "1 reading -> a whole LINE of (I,Q) fit it (phase unknown);  a 2nd reading at a different phase -> just ONE point",
		worldW/2, 80, 2.7, colMuted, zLabel)
	spawnLabel(c, "smp:p2", "Nyquist: fs >= 2 x Rs (>= 2 samples/symbol).   more slots -> wider bandwidth (no free lunch)",
		worldW/2, 84, 2.7, colMuted, zLabel)

	spawnLabel(c, "smp:hint", "Left/Right: symbol rate     Up/Down: samples per symbol     Esc: back",
		worldW/2, 88, 2.6, colMuted, zLabel)
}

// smpStrings builds the six readout strings for a ui state. Every variable field
// uses a fixed-width verb (and the verdict is padded to a constant length) so the
// strings keep their character count across changes — the labels are spawned once
// at that width and only have their texture swapped, never resized.
func smpStrings(ui *uiState) (ts, rs, b, sp, fs, v string) {
	tsMs, rsBd, bHz, fsHz := smpDims(ui.smpNSym, ui.smpSps)
	ts = fmt.Sprintf("Ts  symbol time   = %5.2f ms", tsMs)
	rs = fmt.Sprintf("Rs  symbol rate   = %5d Bd", rsBd)
	b = fmt.Sprintf("B   min bandwidth ~ %5d Hz", bHz)
	sp = fmt.Sprintf("samples / symbol  = %d", ui.smpSps)
	fs = fmt.Sprintf("fs  sample rate   = %6d Hz", fsHz)
	switch {
	case ui.smpSps < 2:
		v = "1 sample: I/Q underdetermined - phase lost"
	case ui.smpSps == 2:
		v = "2 samples: the minimum - recovers I and Q"
	default:
		v = "oversampled: extra samples are redundant"
	}
	return ts, rs, b, sp, fs, fmt.Sprintf("%-42s", v)
}

// bakeSamplingInfo re-renders the readout label textures in place (same lengths,
// so the sprite geometry stays fixed).
func bakeSamplingInfo(c *app.Ctx, ui *uiState) {
	ts, rs, b, sp, fs, v := smpStrings(ui)
	loadLabel(c, "smp:rTs", ts)
	loadLabel(c, "smp:rRs", rs)
	loadLabel(c, "smp:rB", b)
	loadLabel(c, "smp:rSp", sp)
	loadLabel(c, "smp:rFs", fs)
	loadLabel(c, "smp:rV", v)
}

func samplingInput(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	changed := false
	for _, ev := range app.ReadEvents[webgpu.KeyEvent](c) {
		if ev.Action != glfw.Press && ev.Action != glfw.Repeat {
			continue
		}
		switch ev.Key {
		case glfw.KeyRight:
			if ui.smpNSym < smpMaxSym {
				ui.smpNSym++
				changed = true
			}
		case glfw.KeyLeft:
			if ui.smpNSym > 2 {
				ui.smpNSym--
				changed = true
			}
		case glfw.KeyUp:
			if ui.smpSps < smpMaxSps {
				ui.smpSps++
				changed = true
			}
		case glfw.KeyDown:
			if ui.smpSps > 1 {
				ui.smpSps--
				changed = true
			}
		}
	}
	if changed {
		bakeSamplingInfo(c, ui)
	}
}

func samplingDraw(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	nSym, sps := ui.smpNSym, ui.smpSps
	offscreen := func(int) (float32, float32) { return -50, -50 }

	// Continuous carrier: each slot carries its own symbol, so the phase jumps at
	// slot boundaries (unfiltered QPSK). u runs 0..1 across the whole window.
	waveAt := func(uGlobal float64) float64 {
		slot := int(uGlobal * float64(nSym))
		if slot >= nSym {
			slot = nSym - 1
		}
		uLocal := uGlobal*float64(nSym) - float64(slot)
		return passband(smpSymbol(slot), smpCycles, uLocal)
	}
	placeSeries(c, smpWave, func(i int) (float32, float32) {
		u := float64(i) / float64(smpWaveN-1)
		return smpWaveVP.mapPoint(smpData, u, clampf(waveAt(u), -1.39, 1.39))
	})

	// Slot dividers at the interior boundaries.
	placeSeries(c, smpDiv, func(idx int) (float32, float32) {
		if idx >= nSym-1 {
			return -50, -50
		}
		u := float64(idx+1) / float64(nSym)
		x, _ := smpWaveVP.mapPoint(smpData, u, 0)
		return x, smpWaveVP.Y + smpWaveVP.H/2
	})

	// Sample instants: sps evenly-spaced points inside every slot, centred in
	// their sub-interval. Dots ride the wave; ticks sit on the time axis.
	active := nSym * sps
	_, zy := smpWaveVP.mapPoint(smpData, 0, 0)
	sampleU := func(idx int) (uGlobal, uLocal float64, slot int) {
		slot = idx / sps
		k := idx % sps
		uLocal = (float64(k) + 0.5) / float64(sps)
		uGlobal = (float64(slot) + uLocal) / float64(nSym)
		return
	}
	placeSeries(c, smpSamp, func(idx int) (float32, float32) {
		if idx >= active {
			return offscreen(idx)
		}
		uGlobal, uLocal, slot := sampleU(idx)
		v := passband(smpSymbol(slot), smpCycles, uLocal)
		return smpWaveVP.mapPoint(smpData, uGlobal, clampf(v, -1.39, 1.39))
	})
	placeSeries(c, smpTick, func(idx int) (float32, float32) {
		if idx >= active {
			return offscreen(idx)
		}
		uGlobal, _, _ := sampleU(idx)
		x, _ := smpWaveVP.mapPoint(smpData, uGlobal, 0)
		return x, zy
	})

	// Too-few samples turns the dots red — the "phase is lost" warning.
	dotCol := colI
	if sps < 2 {
		dotCol = colErr
	}
	recolorSeries(c, smpSamp, func(int) webgpu.Color { return dotCol })
}
