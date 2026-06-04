package main

import (
	"math"
	"math/bits"
	"testing"
)

// TestQPSKMapping checks the four points sit on the unit circle in the expected
// quadrants and that the bit→sign convention holds.
func TestQPSKMapping(t *testing.T) {
	for idx := 0; idx < 4; idx++ {
		b0, b1 := bitsOf(idx)
		s := qpskMap(b0, b1)
		if mag := math.Hypot(s.I, s.Q); math.Abs(mag-1) > 1e-9 {
			t.Errorf("symbol %02b magnitude = %v, want 1", idx, mag)
		}
		wantINeg := b0 == 1
		wantQNeg := b1 == 1
		if (s.I < 0) != wantINeg || (s.Q < 0) != wantQNeg {
			t.Errorf("symbol %02b = %+v, signs don't match bits", idx, s)
		}
	}
}

// TestRoundTrip modulates each symbol onto a clean carrier, demodulates over one
// symbol period, and checks both the recovered I/Q and the decided bits match.
func TestRoundTrip(t *testing.T) {
	const (
		f                = 5.0 // carrier frequency
		cyclesPerSymbol  = 5   // integer cycles → carrier orthogonality holds
		period           = cyclesPerSymbol / f
		integrationSteps = 4096
	)
	for idx := 0; idx < 4; idx++ {
		b0, b1 := bitsOf(idx)
		sym := qpskMap(b0, b1)

		recv := func(t float64) float64 { return passband(sym, f, t) }
		got := demod(recv, f, period, integrationSteps)

		if math.Abs(got.I-sym.I) > 1e-3 || math.Abs(got.Q-sym.Q) > 1e-3 {
			t.Errorf("symbol %02b: demod = %+v, want %+v", idx, got, sym)
		}
		gb0, gb1 := decide(got)
		if gb0 != b0 || gb1 != b1 {
			t.Errorf("symbol %02b: decided bits = %d%d, want %d%d", idx, gb0, gb1, b0, b1)
		}
	}
}

// TestQAM16 checks the 16-QAM constellation is unit-energy, has 16 distinct
// points, and is Gray-coded (adjacent grid positions differ by one bit).
func TestQAM16(t *testing.T) {
	var energy float64
	seen := map[Symbol]bool{}
	for i := 0; i < 4; i++ {
		for q := 0; q < 4; q++ {
			p := qam16Point(i, q)
			energy += p.I*p.I + p.Q*p.Q
			seen[p] = true
		}
	}
	if len(seen) != 16 {
		t.Errorf("expected 16 distinct points, got %d", len(seen))
	}
	if avg := energy / 16; math.Abs(avg-1) > 1e-9 {
		t.Errorf("average energy = %v, want 1", avg)
	}
	for pos := 0; pos < 3; pos++ {
		if bits.OnesCount(uint(gray2(pos)^gray2(pos+1))) != 1 {
			t.Errorf("gray2(%d) and gray2(%d) differ by more than one bit", pos, pos+1)
		}
	}
}

// TestDecideBoundaries checks the decision regions are the four quadrants.
func TestDecideBoundaries(t *testing.T) {
	cases := []struct {
		s        Symbol
		wb0, wb1 int
	}{
		{Symbol{0.9, 0.1}, 0, 0},
		{Symbol{0.2, -0.8}, 0, 1},
		{Symbol{-0.5, 0.5}, 1, 0},
		{Symbol{-0.3, -0.3}, 1, 1},
	}
	for _, tc := range cases {
		b0, b1 := decide(tc.s)
		if b0 != tc.wb0 || b1 != tc.wb1 {
			t.Errorf("decide(%+v) = %d%d, want %d%d", tc.s, b0, b1, tc.wb0, tc.wb1)
		}
	}
}
