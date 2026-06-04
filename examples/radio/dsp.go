package main

import "math"

// This file is the educational core: the QPSK math, kept pure (no engine deps)
// so it can be unit-tested and so the scenes can call it to drive their
// visualizations. Everything here is plain DSP.

// invSqrt2 places the four QPSK points on the unit circle (|symbol| == 1), so
// the constellation has unit average energy — the textbook normalization.
const invSqrt2 = 0.7071067811865476

// Symbol is one QPSK constellation point: a complex amplitude with in-phase (I)
// and quadrature (Q) components. QPSK carries 2 bits per symbol, so I and Q each
// take one of two values, ±1/√2.
type Symbol struct{ I, Q float64 }

// qpskMap maps a 2-bit value to its constellation point. The mapping is Gray
// coded — the first bit selects the I sign, the second the Q sign — so the four
// quadrants are 00, 01, 11, 10 going around, and neighbouring points differ by a
// single bit. A 0 bit means a positive component, a 1 bit a negative one.
func qpskMap(b0, b1 int) Symbol {
	s := Symbol{I: invSqrt2, Q: invSqrt2}
	if b0 == 1 {
		s.I = -s.I
	}
	if b1 == 1 {
		s.Q = -s.Q
	}
	return s
}

// qpskConstellation returns the four QPSK points indexed by (b0<<1 | b1):
// index 0 = "00", 1 = "01", 2 = "10", 3 = "11".
func qpskConstellation() [4]Symbol {
	var pts [4]Symbol
	for idx := 0; idx < 4; idx++ {
		pts[idx] = qpskMap(idx>>1, idx&1)
	}
	return pts
}

// bitsOf splits a 2-bit symbol index into its (b0, b1) bits.
func bitsOf(idx int) (b0, b1 int) { return (idx >> 1) & 1, idx & 1 }

// indexOf packs two bits back into a 0..3 symbol index.
func indexOf(b0, b1 int) int { return (b0&1)<<1 | (b1 & 1) }

// passband returns the transmitted real-valued signal at time t for a symbol:
//
//	s(t) = I·cos(2πf t) − Q·sin(2πf t)
//
// This is the heart of I/Q modulation: the in-phase component rides a cosine
// carrier and the quadrature component rides a sine carrier 90° out of phase.
// Their sum is a single sinusoid whose amplitude and phase encode the symbol.
func passband(s Symbol, f, t float64) float64 {
	w := 2 * math.Pi * f
	return s.I*math.Cos(w*t) - s.Q*math.Sin(w*t)
}

// inPhase returns the in-phase carrier term I·cos(2πf t) on its own (for plotting
// the two carriers separately).
func inPhase(s Symbol, f, t float64) float64 {
	return s.I * math.Cos(2*math.Pi*f*t)
}

// quadrature returns the quadrature carrier term −Q·sin(2πf t) on its own.
func quadrature(s Symbol, f, t float64) float64 {
	return -s.Q * math.Sin(2*math.Pi*f*t)
}

// demod recovers (I, Q) from a received signal by correlating it against the
// cos / −sin carriers over one symbol period and normalizing. This is coherent
// I/Q demodulation: multiply by each carrier and integrate. Orthogonality of the
// carriers over an integer number of cycles (cyclesPerSymbol whole cycles in
// `period`) makes the two components separable.
//
// recv samples the received signal at a given time; n is the number of
// integration samples across the period.
func demod(recv func(t float64) float64, f, period float64, n int) Symbol {
	w := 2 * math.Pi * f
	dt := period / float64(n)
	var iAcc, qAcc float64
	for k := 0; k < n; k++ {
		t := (float64(k) + 0.5) * dt
		r := recv(t)
		iAcc += r * math.Cos(w*t)
		qAcc += r * -math.Sin(w*t)
	}
	// ∫ r·cos over the period equals I·(period/2); the 2/period factor inverts it.
	scale := 2.0 / float64(n)
	return Symbol{I: iAcc * scale, Q: qAcc * scale}
}

// --- 16-QAM ---------------------------------------------------------------
//
// 16-QAM carries 4 bits per symbol. Two bits choose an I amplitude level and two
// choose a Q level, from {-3,-1,+1,+3}. Unlike QPSK (all points the same
// distance from the origin), here both amplitude and phase vary — the price is
// 16 points packed into the same space, so they tolerate less noise.

// qamLevels are the four per-axis amplitudes before normalization.
var qamLevels = [4]float64{-3, -1, 1, 3}

// qamNorm = sqrt(10) normalizes the constellation to unit average energy: the
// per-axis mean square of qamLevels is 5, so dividing by sqrt(10) makes the mean
// symbol energy (I^2+Q^2 averaged over all 16 points) equal 1.
const qamNorm = 3.1622776601683795

// gray2 maps a grid position (0..3) to its 2-bit Gray code: 0->00, 1->01, 2->11,
// 3->10. Adjacent grid positions differ in exactly one bit, so a small slip into
// a neighbouring level flips only one bit.
func gray2(pos int) int { return pos ^ (pos >> 1) }

// qam16Point maps I/Q grid positions (0..3 each, left-to-right / bottom-to-top)
// to a normalized 16-QAM constellation point.
func qam16Point(iPos, qPos int) Symbol {
	return Symbol{I: qamLevels[iPos] / qamNorm, Q: qamLevels[qPos] / qamNorm}
}

// decide performs the nearest-point (maximum-likelihood) decision for a received
// I/Q estimate, returning the recovered 2 bits. For QPSK this is just the sign of
// each component: positive → 0, negative → 1, matching qpskMap.
func decide(s Symbol) (b0, b1 int) {
	if s.I < 0 {
		b0 = 1
	}
	if s.Q < 0 {
		b1 = 1
	}
	return b0, b1
}
