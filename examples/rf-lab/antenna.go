//go:build !js

package main

import "math"

// Antenna directivity. An isotropic antenna radiates (and receives) equally in every
// direction; a directional one concentrates its gain into a main lobe around its
// boresight azimuth, trading coverage for reach where it is aimed. The pattern here
// is a cos^n power lobe with a small back-lobe floor — enough to teach pointing and
// beamwidth (aim it at the partner for a strong link; off-axis and the SNR collapses)
// without a full antenna model.

const (
	antBackLobe = 0.05        // power floor behind the antenna (~13 dB front-to-back)
	beamDefault = math.Pi / 3 // default half-power beamwidth, radians (60°)
)

// antennaGain returns the linear power-pattern factor (antBackLobe…1) for a signal
// travelling along bearing pathBear (the XZ-plane angle atan2(dz,dx)) relative to an
// antenna aimed at azimuth with the given half-power beamwidth (radians). An
// omnidirectional antenna returns 1 in every direction.
func antennaGain(pathBear, azimuth, beamwidth float64, omni bool) float64 {
	if omni {
		return 1
	}
	align := math.Cos(pathBear - azimuth) // 1 on boresight, −1 directly behind
	if align <= 0 {
		return antBackLobe
	}
	return antBackLobe + (1-antBackLobe)*math.Pow(align, beamSharpness(beamwidth))
}

// beamSharpness is the cos^n exponent that puts the half-power (−3 dB) points at
// ±beamwidth/2, i.e. cos(bw/2)^n = 0.5. Narrower beams yield a larger exponent.
func beamSharpness(beamwidth float64) float64 {
	c := math.Cos(beamwidth / 2)
	switch {
	case c <= 0.001:
		return 1
	case c >= 0.999:
		return 200 // an extremely narrow pencil beam
	default:
		return math.Log(0.5) / math.Log(c)
	}
}
