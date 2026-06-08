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

// rfSystem recomputes the link budget each frame for the focused transmitter →
// receiver pair (the selected device and its first counterpart) and stores it in
// Link: received power (via Friis), Tx–Rx distance, and the SNR against the
// receiver's thermal noise floor. When either side is missing the link is zeroed.
func rfSystem(c *app.Ctx) {
	lab := app.GetResource[Lab](c)
	link := app.GetResource[Link](c)
	if lab == nil || link == nil {
		return
	}
	txd, txPos, okt := focusTx(c, lab)
	rxd, rxPos, okr := focusRx(c, lab)
	if !okt || !okr {
		link.DistM, link.PowerW, link.SNRdB = 0, 0, 0
		return
	}
	d := float64(rxPos.Sub(txPos).Length())
	grx := math.Pow(10, rxd.GainDBi/10) // dBi → linear; Tx assumed isotropic
	pr := freeSpacePowerW(txd.PowerW, txd.FreqHz, 1, grx, d)

	// A block standing on the Tx→Rx line occludes the path, so the reported link
	// budget collapses just as the wave does — more so at higher carriers.
	pr *= losPower(float64(txPos.X), float64(txPos.Z), float64(rxPos.X), float64(rxPos.Z), txd.FreqHz, gatherBlocks(c))

	link.DistM = d
	link.PowerW = pr
	link.SNRdB = sim.SNRdB(pr, rxd.rx(rxPos))
}
