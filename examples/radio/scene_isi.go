package main

import (
	"fmt"
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/webgpu"
)

// Bandwidth scene: what stops a symbol segment from shrinking forever.
//
// Each symbol is a sharp rectangular level (the "sent" step, faint). A real
// channel only passes a finite bandwidth B, which smooths those sharp edges —
// the wave can't change faster than ~B times per second. We model that band
// limit as a fixed-width smoother (a Gaussian edge, via erf): each sent
// rectangle becomes a rounded pulse with a fixed rise/fall time set by B.
//
// While the segment Ts is comfortably wider than the channel's smoothing
// (Ts·B > 1), each rounded pulse still reaches its full level at its own centre
// and has died away by the neighbours' centres — the samples are clean. Pack the
// symbols tighter (add more to the fixed window) and Ts shrinks below 1/B: the
// rounded pulses no longer settle in time, neighbours overlap at the sample
// instants (inter-symbol interference), the decisions drift, and bits flip. That
// overlap — not any rule we imposed — is what enforces the minimum segment size.
//
// Left/Right change how many symbols are packed into the fixed window (shorter
// segments). Watch the eye close and red error dots appear once Ts·B drops below 1.

const (
	isiIdeal = 0 // faint sent step (sharp rectangles)
	isiRecvS = 1 // the band-limited received wave
	isiSamp  = 2 // decision samples at each segment centre
	isiDiv   = 3 // segment dividers

	isiIdealN = 420
	isiRecvN  = 640
	isiMaxSym = 16
	isiMaxDiv = isiMaxSym - 1

	// Channel smoothing width, as a fraction of the window — this is the band
	// limit. isiLimitSym is the most symbols the band can resolve cleanly; pack in
	// more and the segment Ts drops below 1/B and ISI errors begin (here, at 10).
	isiSigma    = 0.075
	isiLimitSym = 9
)

// isiS2 is sigma*sqrt(2), the erf scale (a var because const can't call Sqrt).
var isiS2 = isiSigma * math.Sqrt2

// isiPattern is a fixed bit pattern (1 -> +1, 0 -> -1) chosen to include isolated
// symbols — a lone level flanked by opposite ones — which is the worst case for
// ISI: both neighbours pull the same way, so they're the first decisions to flip.
var isiPattern = [isiMaxSym]int{-1, -1, 1, -1, -1, 1, 1, -1, 1, -1, 1, 1, 1, -1, -1, 1}

var (
	isiVP   = viewport{X: 8, Y: 21, W: 144, H: 32}
	isiData = dataRect{X0: 0, X1: 1, Y0: -1.4, Y1: 1.4}
)

// isiAmp is the sent level of segment n: +1 or -1.
func isiAmp(n int) float64 { return float64(isiPattern[n%isiMaxSym]) }

// isiBox is a band-limited unit rectangle spanning [l, r]: ~1 inside, ~0 outside,
// with edges smoothed over width sigma (the channel's finite bandwidth).
func isiBox(t, l, r float64) float64 {
	return 0.5 * (math.Erf((t-l)/isiS2) - math.Erf((t-r)/isiS2))
}

// isiRecv is the received wave at time t (window units): the sum of every
// segment's band-limited pulse. Overlap between neighbours is the ISI.
func isiRecv(t float64, nSym int) float64 {
	sum := 0.0
	for n := 0; n < nSym; n++ {
		l := float64(n) / float64(nSym)
		r := float64(n+1) / float64(nSym)
		sum += isiAmp(n) * isiBox(t, l, r)
	}
	return sum
}

// isiDecideErrors counts how many segment centres decode to the wrong sign once
// the neighbours' tails have leaked in.
func isiDecideErrors(nSym int) int {
	errs := 0
	for m := 0; m < nSym; m++ {
		center := (float64(m) + 0.5) / float64(nSym)
		dec := 1.0
		if isiRecv(center, nSym) < 0 {
			dec = -1
		}
		if dec != isiAmp(m) {
			errs++
		}
	}
	return errs
}

// isiTsB returns Ts*B, the segment width measured in channel-resolution units:
// the limit spacing (window/isiLimitSym) divided by the current spacing
// (window/nSym). >1 means the segment is wider than the band can resolve (clean);
// <1 is too tight for the band, and ISI sets in.
func isiTsB(nSym int) float64 { return float64(isiLimitSym) / float64(nSym) }

func isiStrings(nSym int) (syms, tsb, errs string) {
	tsbV := isiTsB(nSym)
	status := "<1  segment TOO TIGHT -> ISI"
	if tsbV >= 1 {
		status = ">1  segment fits the band  "
	}
	syms = fmt.Sprintf("symbols packed in window =  %2d", nSym)
	tsb = fmt.Sprintf("Ts x B = %4.2f   %s", tsbV, status)
	errs = fmt.Sprintf("ISI bit errors =  %2d", isiDecideErrors(nSym))
	return
}

func setupBandwidth(c *app.Ctx) {
	cmds := c.Commands()
	ui := app.GetResource[uiState](c)
	if ui.bwNSym < 2 || ui.bwNSym > isiMaxSym {
		ui.bwNSym = 4
	}

	spawnBackButton(c)
	spawnLabel(c, "bw:title", "BANDWIDTH LIMIT  &  ISI", worldW/2, 8, 5, colText, zLabel)
	spawnLabel(c, "bw:sub", "what stops a segment from shrinking forever",
		worldW/2, 13, 2.8, colMuted, zLabel)
	spawnLabel(c, "bw:leg", "faint = sent (sharp)     bright = received (band-limited)     dots = decisions",
		worldW/2, 18, 2.5, colMuted, zLabel)

	// Panel + zero axis.
	spawnSprite(cmds, "sq", isiVP.X-2, isiVP.Y-2, isiVP.W+4, isiVP.H+4, colPanel, zPanel)
	_, zy := isiVP.mapPoint(isiData, 0, 0)
	spawnSprite(cmds, "sq", isiVP.X, zy-0.1, isiVP.W, 0.2, colAxis, zAxis)

	spawnMarkers(cmds, isiDiv, isiMaxDiv, "sq", 0.25, isiVP.H, colGrid, zAxis)
	spawnSeries(cmds, isiIdeal, isiIdealN, 0.7, colGrid)
	spawnSeries(cmds, isiRecvS, isiRecvN, 1.1, colSum)
	spawnSeries(cmds, isiSamp, isiMaxSym, 2.6, colI)

	// Readouts.
	syms, tsb, errs := isiStrings(ui.bwNSym)
	spawnLabelLeft(c, "bw:syms", syms, 12, 59, 3.0, colText, zLabel)
	spawnLabelLeft(c, "bw:tsb", tsb, 12, 64, 3.0, colAccent, zLabel)
	spawnLabelLeft(c, "bw:err", errs, 12, 69, 3.0, colSum, zLabel)

	spawnLabel(c, "bw:p1", "the band-limited wave can't change faster than ~B times/sec; sharp edges get rounded",
		worldW/2, 75, 2.6, colMuted, zLabel)
	spawnLabel(c, "bw:p2", "below Ts ~ 1/B the rounded pulses overlap at the samples (ISI) -> errors. that is the floor.",
		worldW/2, 79, 2.6, colMuted, zLabel)
	spawnLabel(c, "bw:hint", "Left/Right: pack more/fewer symbols (shorter/longer segments)     Esc: back",
		worldW/2, 85, 2.6, colMuted, zLabel)
}

func bandwidthInput(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	changed := false
	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Action != inputs.Press && ev.Action != inputs.Repeat {
			continue
		}
		switch ev.Key {
		case inputs.KeyRight:
			if ui.bwNSym < isiMaxSym {
				ui.bwNSym++
				changed = true
			}
		case inputs.KeyLeft:
			if ui.bwNSym > 2 {
				ui.bwNSym--
				changed = true
			}
		}
	}
	if changed {
		syms, tsb, errs := isiStrings(ui.bwNSym)
		loadLabel(c, "bw:syms", syms)
		loadLabel(c, "bw:tsb", tsb)
		loadLabel(c, "bw:err", errs)
	}
}

func bandwidthDraw(c *app.Ctx) {
	ui := app.GetResource[uiState](c)
	nSym := ui.bwNSym

	// Faint sent step: the sharp rectangular level of whichever segment t is in.
	placeSeries(c, isiIdeal, func(i int) (float32, float32) {
		u := float64(i) / float64(isiIdealN-1)
		slot := int(u * float64(nSym))
		if slot >= nSym {
			slot = nSym - 1
		}
		return isiVP.mapPoint(isiData, u, isiAmp(slot))
	})

	// Bright received wave: the band-limited sum of all segments.
	placeSeries(c, isiRecvS, func(i int) (float32, float32) {
		u := float64(i) / float64(isiRecvN-1)
		return isiVP.mapPoint(isiData, u, clampf(isiRecv(u, nSym), -1.39, 1.39))
	})

	// Decision samples at each segment centre; off-screen for the unused pool.
	placeSeries(c, isiSamp, func(idx int) (float32, float32) {
		if idx >= nSym {
			return -50, -50
		}
		center := (float64(idx) + 0.5) / float64(nSym)
		return isiVP.mapPoint(isiData, center, clampf(isiRecv(center, nSym), -1.39, 1.39))
	})
	// Red where the sampled sign disagrees with the sent symbol (a bit error).
	recolorSeries(c, isiSamp, func(idx int) webgpu.Color {
		if idx >= nSym {
			return colI
		}
		center := (float64(idx) + 0.5) / float64(nSym)
		dec := 1.0
		if isiRecv(center, nSym) < 0 {
			dec = -1
		}
		if dec != isiAmp(idx) {
			return colErr
		}
		return colI
	})

	// Segment dividers.
	placeSeries(c, isiDiv, func(idx int) (float32, float32) {
		if idx >= nSym-1 {
			return -50, -50
		}
		u := float64(idx+1) / float64(nSym)
		x, _ := isiVP.mapPoint(isiData, u, 0)
		return x, isiVP.Y + isiVP.H/2
	})
}
