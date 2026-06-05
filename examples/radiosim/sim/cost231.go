package sim

import "math"

// WICity describes the regular urban canyon the COST-231 Walfisch-Ikegami model
// assumes: building height, base-station and mobile antenna heights, mean
// building separation b, street width w (all meters), and the street orientation
// relative to the direct ray (degrees).
type WICity struct {
	BuildingH   float64
	TxH         float64 // base station antenna height
	RxH         float64 // mobile antenna height
	BuildingSep float64 // mean building separation b
	StreetW     float64 // street width w
	OriDeg      float64 // street orientation angle φ (deg)
}

// DefaultWICity is a typical medium-city canyon.
func DefaultWICity() WICity {
	return WICity{BuildingH: 20, TxH: 30, RxH: 1.5, BuildingSep: 40, StreetW: 20, OriDeg: 90}
}

// Cost231WI returns the non-line-of-sight COST-231 Walfisch-Ikegami median path
// loss in dB at distance d (km) and frequency f (MHz). It sums free-space loss,
// the rooftop-to-street diffraction term Lrts, and the multiscreen diffraction
// term Lmsd, per ITU-R / COST-231. It is the system-level reference the
// site-specific ray model is sanity-checked against over a regular street.
func Cost231WI(c WICity, dKm, fMHz float64) float64 {
	l0 := 32.4 + 20*math.Log10(dKm) + 20*math.Log10(fMHz)

	dhm := c.BuildingH - c.RxH // mobile below rooftop
	// Street-orientation correction Lori.
	phi := c.OriDeg
	var lori float64
	switch {
	case phi < 35:
		lori = -10 + 0.354*phi
	case phi < 55:
		lori = 2.5 + 0.075*(phi-35)
	default:
		lori = 4.0 - 0.114*(phi-55)
	}
	lrts := -16.9 - 10*math.Log10(c.StreetW) + 10*math.Log10(fMHz) + 20*math.Log10(dhm) + lori

	dhb := c.TxH - c.BuildingH // base above rooftop
	var lbsh, ka, kd, kf float64
	if c.TxH > c.BuildingH {
		lbsh = -18 * math.Log10(1+dhb)
		ka = 54
	} else {
		lbsh = 0
		if dKm >= 0.5 {
			ka = 54 - 0.8*dhb
		} else {
			ka = 54 - 0.8*dhb*dKm/0.5
		}
	}
	if c.TxH > c.BuildingH {
		kd = 18
	} else {
		kd = 18 - 15*dhb/c.BuildingH
	}
	// Medium city / suburban centre coefficient.
	kf = -4 + 1.5*(fMHz/925-1)

	lmsd := lbsh + ka + kd*math.Log10(dKm) + kf*math.Log10(fMHz) - 9*math.Log10(c.BuildingSep)

	lb := l0
	if lrts+lmsd > 0 {
		lb += lrts + lmsd
	}
	return lb
}
