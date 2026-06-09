package main

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
)

// freeSpacePowerW returns the Friis free-space received power in watts:
//
//	Pr = Pt · Gtx · Grx · (λ / (4π·d))²
//
// with λ = c/f. Gains are linear (not dB). It is the pure core of the link
// budget — unit-tested in rf_test.go. A non-positive distance or frequency yields
// 0 (no meaningful link).
func freeSpacePowerW(powerW, freqHz, gainTx, gainRx, distM float64) float64 {
	if distM <= 0 || freqHz <= 0 {
		return 0
	}
	lambda := sim.SpeedOfLight / freqHz
	fspl := lambda / (4 * math.Pi * distM)
	return powerW * gainTx * gainRx * fspl * fspl
}

// rfSystem fills in the only part of the link readout that is pure geometry: the
// Tx–Rx distance for the focused pair (the selected device and its first
// counterpart). The signal quantities — received power and SNR — are no longer
// recomputed here; they are measured from the actual sampled wave by fieldSystem
// (Link.PowerW / SNRdB / EVMdB), so the panel can never disagree with what the
// decoder and constellation experience. When either side is missing, distance is
// zeroed.
func rfSystem(c *app.Ctx) {
	lab := app.GetResource[Lab](c)
	link := app.GetResource[Link](c)
	if lab == nil || link == nil {
		return
	}
	_, txPos, okt := focusTx(c, lab)
	_, rxPos, okr := focusRx(c, lab)
	if !okt || !okr {
		link.DistM = 0
		return
	}
	link.DistM = float64(rxPos.Sub(txPos).Length())
}
