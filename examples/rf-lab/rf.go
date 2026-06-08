package main

import (
	"math"
	"math/cmplx"

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
	blocks := gatherBlocks(c)
	pr := freeSpacePowerW(txd.PowerW, txd.FreqHz, 1, grx, d)

	// A block standing on the Tx→Rx line occludes the path, so the reported link
	// budget collapses just as the wave does — more so at higher carriers.
	pr *= losPower(float64(txPos.X), float64(txPos.Z), float64(rxPos.X), float64(rxPos.Z), txd.FreqHz, blocks)

	// With multipath on, the reflected copies add coherently to the direct path:
	// the received power is |Σ √Pr_path · e^{−jk(Δlength)}|², so the reported Pr/SNR
	// fade in and out as the path-length differences sweep through wavelengths when
	// the receiver or a wall moves.
	ch := app.GetResource[Channel](c)
	if ch != nil && ch.Multipath && len(blocks) > 0 {
		k := 2 * math.Pi * txd.FreqHz / sim.SpeedOfLight
		h := complex(math.Sqrt(pr), 0) // direct path is the phase reference
		nodes := buildImages(p2{x: float64(txPos.X), z: float64(txPos.Z)}, blockFaces(blocks), ch.MaxOrder)
		var taps []rfTap
		collectReflections(
			p2{x: float64(txPos.X), z: float64(txPos.Z)},
			p2{x: float64(rxPos.X), z: float64(rxPos.Z)},
			txd.FreqHz, blocks, nodes, ch.Reflectivity, ch.MaxOrder, &taps)
		for _, tp := range taps {
			prp := freeSpacePowerW(txd.PowerW, txd.FreqHz, 1, grx, tp.length) * tp.atten * tp.atten
			h += complex(math.Sqrt(prp), 0) * cmplx.Exp(complex(0, -k*(tp.length-d)))
		}
		pr = real(h)*real(h) + imag(h)*imag(h)
	}

	link.DistM = d
	link.PowerW = pr
	link.SNRdB = sim.SNRdB(pr, rxd.rx(rxPos))
}
