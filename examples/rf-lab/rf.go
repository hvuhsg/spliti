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

// rfSystem recomputes the link budget each frame from the current Lab state and
// stores it in Link: received power (via Friis), Tx–Rx distance, and the SNR
// against the receiver's thermal noise floor.
func rfSystem(c *app.Ctx) {
	lab := app.GetResource[Lab](c)
	link := app.GetResource[Link](c)
	txd := txDevice(c)
	rxd := rxDevice(c)
	if lab == nil || link == nil || txd == nil || rxd == nil {
		return
	}
	d := float64(lab.RxPos.Sub(lab.TxPos).Length())
	grx := math.Pow(10, rxd.GainDBi/10) // dBi → linear; Tx assumed isotropic
	pr := freeSpacePowerW(txd.PowerW, txd.FreqHz, 1, grx, d)

	link.DistM = d
	link.PowerW = pr
	link.SNRdB = sim.SNRdB(pr, rxd.rx(lab.RxPos))
}
