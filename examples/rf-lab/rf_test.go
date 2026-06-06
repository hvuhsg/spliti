package main

import (
	"math"
	"testing"

	"github.com/hvuhsg/spliti/examples/radiosim/sim"
)

// dB returns 10·log10(a/b).
func dB(a, b float64) float64 { return 10 * math.Log10(a/b) }

// TestFreeSpaceDistanceDoubling verifies the inverse-square law: doubling the
// distance drops received power by 6.02 dB.
func TestFreeSpaceDistanceDoubling(t *testing.T) {
	p1 := freeSpacePowerW(1, 1e9, 1, 1, 100)
	p2 := freeSpacePowerW(1, 1e9, 1, 1, 200)
	if got := dB(p1, p2); math.Abs(got-6.0206) > 1e-3 {
		t.Fatalf("distance doubling: got %.4f dB, want 6.0206", got)
	}
}

// TestFreeSpaceFrequencyDoubling verifies that doubling the frequency (halving the
// wavelength) drops received power by 6.02 dB.
func TestFreeSpaceFrequencyDoubling(t *testing.T) {
	p1 := freeSpacePowerW(1, 1e9, 1, 1, 100)
	p2 := freeSpacePowerW(1, 2e9, 1, 1, 100)
	if got := dB(p1, p2); math.Abs(got-6.0206) > 1e-3 {
		t.Fatalf("frequency doubling: got %.4f dB, want 6.0206", got)
	}
}

// TestFreeSpaceClosedForm checks an absolute value against the hand-computed
// Friis result for Pt=1 W, f=1 GHz, d=100 m, unit gains.
func TestFreeSpaceClosedForm(t *testing.T) {
	got := freeSpacePowerW(1, 1e9, 1, 1, 100)
	lambda := sim.SpeedOfLight / 1e9
	fspl := lambda / (4 * math.Pi * 100)
	want := fspl * fspl
	if math.Abs(got-want)/want > 1e-9 {
		t.Fatalf("closed form: got %g, want %g", got, want)
	}
	// Sanity: Pt = 1 W = +30 dBm, FSPL(1 GHz, 100 m) ~ 72.4 dB, so Pr ~ -42.4 dBm.
	if d := sim.DBm(got); math.Abs(d-(-42.45)) > 0.1 {
		t.Fatalf("closed form dBm: got %.2f, want ~-42.45", d)
	}
}

// TestZeroDistance returns no link rather than dividing by zero.
func TestZeroDistance(t *testing.T) {
	if got := freeSpacePowerW(1, 1e9, 1, 1, 0); got != 0 {
		t.Fatalf("zero distance: got %g, want 0", got)
	}
}
