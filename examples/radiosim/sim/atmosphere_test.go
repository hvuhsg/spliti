package sim

import (
	"math"
	"testing"
)

// Clear air is essentially transparent below 6 GHz: a small fraction of a dB/km.
func TestAtmosphereClearSub6(t *testing.T) {
	clear := Weather{}
	for _, fG := range []float64{0.9, 2.4, 5.8} {
		if g := clear.SpecificAttenDBperKM(fG * 1e9); g > 0.05 {
			t.Errorf("clear-air attenuation at %g GHz = %g dB/km, want ~0", fG, g)
		}
	}
}

// The 60 GHz oxygen line is a strong, locally-peaked absorption.
func TestAtmosphereOxygenPeak(t *testing.T) {
	clear := Weather{}
	peak := clear.SpecificAttenDBperKM(60e9)
	if peak < 10 {
		t.Errorf("60 GHz oxygen attenuation = %g dB/km, want > 10", peak)
	}
	if peak <= clear.SpecificAttenDBperKM(50e9) || peak <= clear.SpecificAttenDBperKM(70e9) {
		t.Error("60 GHz should be a local attenuation peak")
	}
}

// Rain at 28 GHz, 25 mm/h gives a few dB/km (ITU-R P.838), well above clear air,
// and grows monotonically with rain rate.
func TestAtmosphereRain(t *testing.T) {
	clear := Weather{}
	wet := Weather{RainRateMMH: 25}
	dryAtt := clear.SpecificAttenDBperKM(28e9)
	wetAtt := wet.SpecificAttenDBperKM(28e9)

	rainOnly := wetAtt - dryAtt
	if rainOnly < 2 || rainOnly > 10 {
		t.Errorf("28 GHz rain (25 mm/h) = %g dB/km, want ~3-5", rainOnly)
	}
	if wetAtt <= dryAtt {
		t.Error("rain should increase attenuation")
	}

	prev := 0.0
	for _, r := range []float64{1, 5, 25, 50, 100} {
		g := Weather{RainRateMMH: r}.SpecificAttenDBperKM(28e9)
		if g < prev {
			t.Errorf("rain attenuation not monotonic at R=%g: %g < %g", r, g, prev)
		}
		prev = g
	}
}

// The P.838 coefficients are in the right ballpark at 28 GHz (horizontal pol).
func TestP838Coefficients(t *testing.T) {
	k, a := p838(28)
	if k < 0.1 || k > 0.25 {
		t.Errorf("k(28 GHz) = %g, want ~0.16", k)
	}
	if a < 0.95 || a > 1.1 {
		t.Errorf("α(28 GHz) = %g, want ~1.04", a)
	}
}

// Atmospheric loss reduces received power, and far more at mmWave than sub-6 GHz
// over the same geometry under heavy rain.
func TestAtmosphereReducesPower(t *testing.T) {
	box := []Face{} // free space, long link
	scClear := &Scene{Faces: box}
	scRain := &Scene{Faces: box, Weather: Weather{RainRateMMH: 50}}

	tx := func(f float64) Tx { return Tx{Pos: m3(0, 10, 0), PowerW: 1, FreqHz: f} }
	rx := Rx{Pos: m3(2000, 10, 0)} // 2 km link
	var eng Engine = ImageEngine{}
	cfg := Config{MaxOrder: 0}

	// At 2.4 GHz rain barely matters; at 28 GHz it bites hard.
	p1, _ := Received(eng.Paths(tx(2.4e9), rx, scClear, cfg))
	p1r, _ := Received(eng.Paths(tx(2.4e9), rx, scRain, cfg))
	p2, _ := Received(eng.Paths(tx(28e9), rx, scClear, cfg))
	p2r, _ := Received(eng.Paths(tx(28e9), rx, scRain, cfg))

	loss24 := math.Abs(DBm(p1r) - DBm(p1))
	loss28 := math.Abs(DBm(p2r) - DBm(p2))
	if loss28 <= loss24 {
		t.Errorf("rain loss should be worse at 28 GHz (%.1f dB) than 2.4 GHz (%.1f dB)", loss28, loss24)
	}
}
