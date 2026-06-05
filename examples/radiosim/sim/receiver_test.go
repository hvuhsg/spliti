package sim

import (
	"math"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// The thermal noise floor matches −174 dBm/Hz + 10log₁₀(B) + NF.
func TestNoiseFloor(t *testing.T) {
	rx := Rx{BandwidthHz: 1, TempK: 290, NoiseFigDB: 0}
	if d := DBm(rx.NoiseFloorW()); math.Abs(d-(-173.98)) > 0.1 {
		t.Errorf("kTB at 1 Hz = %.2f dBm, want ~-174", d)
	}
	// Widening the band 1 Hz → 1 MHz adds 60 dB; +5 dB noise figure adds 5 dB.
	wide := Rx{BandwidthHz: 1e6, TempK: 290, NoiseFigDB: 5}
	if d := DBm(wide.NoiseFloorW()); math.Abs(d-(-173.98+60+5)) > 0.1 {
		t.Errorf("kTB at 1 MHz, NF 5 = %.2f dBm, want ~-108.98", d)
	}
}

// BPSK BER equals Q(√(2·Eb/N0)) at representative SNRs.
func TestBPSKBer(t *testing.T) {
	for _, snrdB := range []float64{0, 4, 8} {
		ebn0 := math.Pow(10, snrdB/10)
		want := 0.5 * math.Erfc(math.Sqrt(2*ebn0)/math.Sqrt2)
		got := BER(snrdB, BPSK)
		if math.Abs(got-want) > 1e-12 {
			t.Errorf("BER(%g dB) = %g, want %g", snrdB, got, want)
		}
	}
	// Higher-order QAM needs more SNR for the same BER than BPSK.
	if BER(10, QAM64) <= BER(10, BPSK) {
		t.Error("QAM64 should have a worse BER than BPSK at the same SNR")
	}
}

// SNR and sensitivity are mutually consistent: the received power that just meets
// a target BER produces an SNR whose BER equals the target.
func TestSensitivityRoundTrip(t *testing.T) {
	rx := Rx{BandwidthHz: 1e6, TempK: 290, NoiseFigDB: 6, Mod: QPSK}
	const target = 1e-4

	sensDBm := rx.SensitivityDBm(target)
	signalW := 1e-3 * math.Pow(10, sensDBm/10) // dBm → W
	snr := SNRdB(signalW, rx)
	if ber := BER(snr, rx.Mod); math.Abs(ber-target)/target > 0.05 {
		t.Errorf("at sensitivity, BER = %g, want ~%g (SNR %.2f dB)", ber, target, snr)
	}
}

// The half-wave dipole peaks broadside at ~2.15 dBi (1.64 linear) and nulls along
// its axis; the isotropic antenna is unity everywhere.
func TestAntennaPatterns(t *testing.T) {
	iso := Antenna{Kind: Isotropic}
	if g := iso.Gain(m3(1, 0, 0)); g != 1 {
		t.Errorf("isotropic gain = %v, want 1", g)
	}

	dip := Antenna{Kind: HalfWaveDipole, Boresight: m3(0, 0, 1)} // axis +Z
	if g := dip.Gain(m3(1, 0, 0)); math.Abs(g-1.64) > 0.02 {     // broadside
		t.Errorf("dipole broadside gain = %v, want ~1.64", g)
	}
	if g := dip.Gain(m3(0, 0, 1)); g > 1e-3 { // along the axis
		t.Errorf("dipole axial gain = %v, want ~0", g)
	}

	dir := Antenna{Kind: Directional, Boresight: m3(1, 0, 0), HPBWdeg: 30}
	gMax := dir.Gain(m3(1, 0, 0))
	half := dir.Gain(rotZ(m3(1, 0, 0), 15*math.Pi/180)) // half-beamwidth off
	if gMax <= 1 {
		t.Errorf("directional boresight gain = %v, want > 1", gMax)
	}
	if r := half / gMax; math.Abs(r-0.5) > 0.05 {
		t.Errorf("directional gain at HPBW/2 = %v of peak, want ~0.5", r)
	}
}

// Polarization mismatch is 1 for aligned, 0 for orthogonal, ½ at 45°.
func TestPolMismatch(t *testing.T) {
	v := Antenna{Pol: m3(0, 1, 0)}
	h := Antenna{Pol: m3(1, 0, 0)}
	d := Antenna{Pol: m3(1, 1, 0)}
	if g := PolMismatch(v, v); math.Abs(g-1) > 1e-9 {
		t.Errorf("aligned = %v, want 1", g)
	}
	if g := PolMismatch(v, h); g > 1e-9 {
		t.Errorf("orthogonal = %v, want 0", g)
	}
	if g := PolMismatch(v, d); math.Abs(g-0.5) > 1e-6 {
		t.Errorf("45° = %v, want 0.5", g)
	}
	// Unspecified polarization couples perfectly.
	if g := PolMismatch(Antenna{}, v); g != 1 {
		t.Errorf("unspecified = %v, want 1", g)
	}
}

// rotZ rotates a vector about the +Z axis by angle (radians).
func rotZ(v m.Vec3, ang float64) m.Vec3 {
	c, s := float32(math.Cos(ang)), float32(math.Sin(ang))
	return m3(v.X*c-v.Y*s, v.X*s+v.Y*c, v.Z)
}
