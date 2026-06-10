package main

import (
	"fmt"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
)

// Carrier scene: the "more peaks = more data?" trap, answered head-on.
//
// Two rows show the SAME message (same symbols, changing at the SAME slot
// boundaries) on two different carriers — a slow one with few peaks per symbol
// and a fast one with many. The fast row clearly has far more peaks per second,
// yet both decode to the identical bit string, and the symbols/sec and bits/sec
// readouts are identical. The information only ever changes at a slot boundary
// (once per symbol); every peak in between just repeats the current symbol, so a
// faster carrier adds peaks but no information.
//
// Up/Down changes the fast row's carrier (peaks/symbol) -> peaks/sec moves but
// bits/sec does NOT. Left/Right changes the symbol rate, shared by both rows ->
// THAT is what moves bits/sec. So the lever is symbol rate (bandwidth), not
// carrier frequency.

const (
	ckWaveA = 0 // slow-carrier wave
	ckWaveB = 1 // fast-carrier wave
	ckDivA  = 2 // slot dividers, slow row
	ckDivB  = 3 // slot dividers, fast row

	ckWaveAN = 480 // dots in the slow wave
	ckWaveBN = 900 // dots in the fast wave (needs more, it has many cycles)
	ckMaxSym = 6
	ckMaxDiv = ckMaxSym - 1

	ckCarA       = 1.0 // slow carrier: cycles per symbol (fixed)
	ckMaxCarB    = 8   // fast carrier: most cycles per symbol
	ckWindowMs   = 8.0 // the shared fixed time window, in ms
	ckBitsPerSym = 2   // QPSK
)

var (
	ckAVP  = viewport{X: 8, Y: 20, W: 144, H: 13}
	ckBVP  = viewport{X: 8, Y: 44, W: 144, H: 13}
	ckData = dataRect{X0: 0, X1: 1, Y0: -1.4, Y1: 1.4}
)

// ckBits is the decoded bit string for nSym slots, e.g. "01 00 11 10".
func ckBits(nSym int) string {
	out := make([]byte, 0, 3*ckMaxSym)
	for s := 0; s < nSym; s++ {
		if s > 0 {
			out = append(out, ' ')
		}
		b0, b1 := bitsOf(smpSymIdx(s))
		out = append(out, byte('0'+b0), byte('0'+b1))
	}
	return string(out)
}

// ckStrings builds the readout strings. Fixed-width verbs (and a padded bit
// string) keep every string's length constant so the labels never need resizing.
func ckStrings(ui *uiState) (dec, peaks, syms, bits string) {
	rs := 1000 * ui.ckNSym / int(ckWindowMs) // symbol rate, Bd
	peaksA := int(ckCarA) * rs
	peaksB := ui.ckCarB * rs
	dec = fmt.Sprintf("decoded:  %-17s", ckBits(ui.ckNSym))
	peaks = fmt.Sprintf("peaks/sec    slow %5d    fast %5d   (x%d)", peaksA, peaksB, ui.ckCarB)
	syms = fmt.Sprintf("symbols/sec  %5d    - SAME for both rows", rs)
	bits = fmt.Sprintf("bits/sec     %5d    - SAME (carrier did not change it)", ckBitsPerSym*rs)
	return
}

func setupCarrier(c *app.Ctx) {
	cmds := c.Commands()
	ui := app.GetResource[uiState](c)
	if ui.ckNSym < 2 || ui.ckNSym > ckMaxSym {
		ui.ckNSym = 4
	}
	if ui.ckCarB < 1 || ui.ckCarB > ckMaxCarB {
		ui.ckCarB = 4
	}

	spawnBackButton(c)
	spawnLabel(c, "ck:title", "MORE PEAKS != MORE DATA", worldW/2, 8, 5, colText, zLabel)
	spawnLabel(c, "ck:sub", "same message, two carriers - the fast one has more peaks, not more bits",
		worldW/2, 14, 2.8, colMuted, zLabel)

	carrierPanel(c, "ck:la", ckAVP, "slow carrier  -  few peaks / symbol")
	carrierPanel(c, "ck:lb", ckBVP, "fast carrier  -  many peaks / symbol")

	// Dividers (accent — the only places information actually changes), waves.
	spawnMarkers(cmds, ckDivA, ckMaxDiv, "sq", 0.3, ckAVP.H, colAccent, zRing)
	spawnMarkers(cmds, ckDivB, ckMaxDiv, "sq", 0.3, ckBVP.H, colAccent, zRing)
	spawnSeries(cmds, ckWaveA, ckWaveAN, 1.0, colSum)
	spawnSeries(cmds, ckWaveB, ckWaveBN, 1.0, colI)

	// Decoded bits under each row — identical, that's the whole point.
	dec, peaks, syms, bits := ckStrings(ui)
	spawnLabelLeft(c, "ck:decA", dec, 46, 36, 3.0, colHi, zLabel)
	spawnLabelLeft(c, "ck:decB", dec, 46, 60, 3.0, colHi, zLabel)

	// Readouts: peaks differ, symbols/bits do not.
	spawnLabelLeft(c, "ck:peaks", peaks, 12, 65, 3.0, colQ, zLabel)
	spawnLabelLeft(c, "ck:syms", syms, 12, 69, 3.0, colAccent, zLabel)
	spawnLabelLeft(c, "ck:bits", bits, 12, 73, 3.0, colSum, zLabel)

	spawnLabel(c, "ck:p1", "info changes only at the dividers (once / symbol); peaks in between just repeat it",
		worldW/2, 78, 2.6, colMuted, zLabel)
	spawnLabel(c, "ck:hint", "Up/Down: fast-carrier peaks (no effect on bits)    Left/Right: symbol rate (the real lever)    Esc: back",
		worldW/2, 84, 2.4, colMuted, zLabel)
}

// carrierPanel draws a labelled wave panel with a zero axis.
func carrierPanel(c *app.Ctx, ref string, vp viewport, label string) {
	cmds := c.Commands()
	spawnSprite(cmds, "sq", vp.X-2, vp.Y-2, vp.W+4, vp.H+4, colPanel, zPanel)
	_, zy := vp.mapPoint(ckData, 0, 0)
	spawnSprite(cmds, "sq", vp.X, zy-0.1, vp.W, 0.2, colAxis, zAxis)
	spawnLabelLeft(c, ref, label, vp.X+1, vp.Y-3, 2.7, colMuted, zLabel)
}

func carrierInput(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	changed := false
	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Action != inputs.Press && ev.Action != inputs.Repeat {
			continue
		}
		switch ev.Key {
		case inputs.KeyUp:
			if ui.ckCarB < ckMaxCarB {
				ui.ckCarB++
				changed = true
			}
		case inputs.KeyDown:
			if ui.ckCarB > 1 {
				ui.ckCarB--
				changed = true
			}
		case inputs.KeyRight:
			if ui.ckNSym < ckMaxSym {
				ui.ckNSym++
				changed = true
			}
		case inputs.KeyLeft:
			if ui.ckNSym > 2 {
				ui.ckNSym--
				changed = true
			}
		}
	}
	if changed {
		dec, peaks, syms, bits := ckStrings(ui)
		loadLabel(c, "ck:decA", dec)
		loadLabel(c, "ck:decB", dec)
		loadLabel(c, "ck:peaks", peaks)
		loadLabel(c, "ck:syms", syms)
		loadLabel(c, "ck:bits", bits)
	}
}

func carrierDraw(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	nSym := ui.ckNSym

	// One slot's symbol, sampled at fraction uGlobal of the window, on a carrier of
	// `cyc` cycles per symbol. Both rows carry the same symbols; only `cyc` differs.
	waveAt := func(uGlobal, cyc float64) float64 {
		slot := int(uGlobal * float64(nSym))
		if slot >= nSym {
			slot = nSym - 1
		}
		uLocal := uGlobal*float64(nSym) - float64(slot)
		return passband(smpSymbol(slot), cyc, uLocal)
	}
	row := func(series, n int, vp viewport, cyc float64) {
		placeSeries(c, series, func(i int) (float32, float32) {
			u := float64(i) / float64(n-1)
			return vp.mapPoint(ckData, u, clampf(waveAt(u, cyc), -1.39, 1.39))
		})
	}
	row(ckWaveA, ckWaveAN, ckAVP, ckCarA)
	row(ckWaveB, ckWaveBN, ckBVP, float64(ui.ckCarB))

	// Slot dividers at the interior boundaries (same x in both rows: same symbol
	// timing). Off-screen for the unused pool entries.
	divs := func(series int, vp viewport) {
		placeSeries(c, series, func(idx int) (float32, float32) {
			if idx >= nSym-1 {
				return -50, -50
			}
			u := float64(idx+1) / float64(nSym)
			x, _ := vp.mapPoint(ckData, u, 0)
			return x, vp.Y + vp.H/2
		})
	}
	divs(ckDivA, ckAVP)
	divs(ckDivB, ckBVP)
}
