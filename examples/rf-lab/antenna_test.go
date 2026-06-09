package main

import (
	"math"
	"testing"
)

// TestAntennaOmni verifies an omnidirectional antenna has unit gain in every
// direction, so toggling directivity off restores the isotropic link exactly.
func TestAntennaOmni(t *testing.T) {
	for _, bear := range []float64{0, 1, math.Pi / 2, math.Pi, -2} {
		if g := antennaGain(bear, 0.7, beamDefault, true); g != 1 {
			t.Errorf("omni gain at %.2f = %g, want 1", bear, g)
		}
	}
}

// TestAntennaBoresightPeak verifies the gain peaks at 1 on boresight regardless of
// where the antenna is aimed.
func TestAntennaBoresightPeak(t *testing.T) {
	for _, az := range []float64{0, 1, -2, math.Pi} {
		if g := antennaGain(az, az, beamDefault, false); math.Abs(g-1) > 1e-9 {
			t.Errorf("boresight gain (az=%.2f) = %g, want 1", az, g)
		}
	}
}

// TestAntennaBackLobe verifies a signal arriving from directly behind the antenna is
// attenuated to the back-lobe floor.
func TestAntennaBackLobe(t *testing.T) {
	if g := antennaGain(math.Pi, 0, beamDefault, false); g != antBackLobe {
		t.Errorf("back-lobe gain = %g, want %g", g, antBackLobe)
	}
}

// TestAntennaMonotonic verifies the lobe falls off monotonically from boresight
// toward the back.
func TestAntennaMonotonic(t *testing.T) {
	prev := antennaGain(0, 0, beamDefault, false)
	for deg := 10; deg <= 180; deg += 10 {
		g := antennaGain(float64(deg)*math.Pi/180, 0, beamDefault, false)
		if g > prev+1e-12 {
			t.Errorf("gain rose at %d deg: %g > %g", deg, g, prev)
		}
		prev = g
	}
}

// TestAntennaNarrowerIsTighter verifies a narrower beam has less gain off-axis than a
// wide one at the same angle — the coverage-for-reach trade.
func TestAntennaNarrowerIsTighter(t *testing.T) {
	off := 40 * math.Pi / 180
	narrow := antennaGain(off, 0, 30*math.Pi/180, false)
	wide := antennaGain(off, 0, 120*math.Pi/180, false)
	if !(narrow < wide) {
		t.Errorf("narrow beam off-axis gain %g should be < wide %g", narrow, wide)
	}
}

// TestAntennaHalfPower verifies the lobe term hits −3 dB (half power) at ±beamwidth/2,
// which is what beamSharpness is solved for.
func TestAntennaHalfPower(t *testing.T) {
	bw := 60 * math.Pi / 180
	g := antennaGain(bw/2, 0, bw, false)
	lobe := (g - antBackLobe) / (1 - antBackLobe) // strip the back-lobe floor
	if math.Abs(lobe-0.5) > 1e-9 {
		t.Errorf("lobe at half-beamwidth = %g, want 0.5", lobe)
	}
}
