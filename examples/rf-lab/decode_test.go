//go:build !js

package main

import (
	"math"
	"testing"

	"github.com/hvuhsg/spliti/examples/radiosim/sim"
)

// --- bitMeter ---

// TestBitMeterRate verifies the sliding window reports bits-in-window / window. The
// meter observes a cumulative count that grows, so deliveries are the per-step growth.
func TestBitMeterRate(t *testing.T) {
	m := bitMeter{win: 4.0}
	m.observe(0.0, 0)  // prime: no delivery on the first observation
	m.observe(1.0, 8)  // +8 bits
	m.observe(2.0, 16) // +8 bits
	if got, want := m.rate(), 16.0/4.0; got != want {
		t.Errorf("rate = %g, want %g", got, want)
	}
}

// TestBitMeterAgesOut verifies deliveries older than the window stop counting, so the
// rate falls to zero once reception stops.
func TestBitMeterAgesOut(t *testing.T) {
	m := bitMeter{win: 4.0}
	m.observe(0.0, 0)
	m.observe(1.0, 8)
	// Advance well past the window with no new bits (cumulative unchanged).
	m.observe(10.0, 8)
	if got := m.rate(); got != 0 {
		t.Errorf("rate after silence = %g, want 0", got)
	}
}

// TestBitMeterIgnoresDrops verifies a decrease in the cumulative count (a message
// loop wrap, or a reset) is not counted as a delivery — only genuine growth is.
func TestBitMeterIgnoresDrops(t *testing.T) {
	m := bitMeter{win: 4.0}
	m.observe(0.0, 0)
	m.observe(1.0, 16)
	m.observe(2.0, 0) // loop wrapped back to zero — not a delivery
	if got, want := m.rate(), 16.0/4.0; got != want {
		t.Errorf("rate = %g, want %g (the wrap must not add bits)", got, want)
	}
}

// --- pre-FEC channel BER ---

// TestBERFracNoneYet verifies berFrac reports "no measurement" (-1) before any
// symbol has been observed.
func TestBERFracNoneYet(t *testing.T) {
	if got := newDecode().berFrac(); got != -1 {
		t.Errorf("berFrac with no data = %g, want -1", got)
	}
}

// TestBERFracCleanChannel verifies a channel that decides every symbol correctly
// measures a zero bit-error rate.
func TestBERFracCleanChannel(t *testing.T) {
	d := newDecode()
	for v := 0; v < 4; v++ {
		d.observeBER(v, v, bitsPerSymbol(sim.QPSK)) // tx == rx: no errors
	}
	if got := d.berFrac(); got != 0 {
		t.Errorf("clean BER = %g, want 0", got)
	}
}

// TestBERFracSingleSymbol verifies the first observation yields an exact ratio (the
// EWMA seeds to it), so one bit wrong out of two reads as 0.5.
func TestBERFracSingleSymbol(t *testing.T) {
	d := newDecode()
	d.observeBER(0b00, 0b01, 2) // one of two bits differs
	if got := d.berFrac(); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("single-symbol BER = %g, want 0.5", got)
	}
}

// TestBERFracTracksErrors verifies a steadily error-laden channel settles to a
// non-zero rate bounded by 1, and a worse channel reads higher than a milder one.
func TestBERFracTracksErrors(t *testing.T) {
	mild := newDecode()
	harsh := newDecode()
	for i := 0; i < 200; i++ {
		mild.observeBER(0b0000, 0b0001, 4)  // 1 of 4 bits wrong
		harsh.observeBER(0b0000, 0b0111, 4) // 3 of 4 bits wrong
	}
	m, h := mild.berFrac(), harsh.berFrac()
	if !(m > 0 && m < h && h <= 1) {
		t.Errorf("expected 0 < mild(%g) < harsh(%g) <= 1", m, h)
	}
}

// TestDecodeResetClearsBER verifies reset returns the BER estimate to "no
// measurement", so a reconnected link starts fresh.
func TestDecodeResetClearsBER(t *testing.T) {
	d := newDecode()
	d.observeBER(0b00, 0b11, 2)
	d.reset()
	if got := d.berFrac(); got != -1 {
		t.Errorf("berFrac after reset = %g, want -1", got)
	}
}

// --- FEC repair rate ---

// TestCorrectedRateReflectsFixes verifies render counts the errors the FEC repaired
// and measure turns that into a non-zero repair rate, which a clean channel leaves at
// zero. It drives the decoder the way fieldSystem does: raw coded bits in, then
// render + measure.
func TestCorrectedRateReflectsFixes(t *testing.T) {
	fec := &Node{Kind: KindErrorCorrect, Code: ECCHamming}

	clean := newDecode()
	clean.raw = hammingEncode([]int{1, 0, 1, 1})
	clean.render(fec, false)
	clean.measure(1.0)
	if clean.corrRate != 0 {
		t.Errorf("clean channel repair rate = %g, want 0", clean.corrRate)
	}

	noisy := newDecode()
	noisy.render(fec, false) // first frame: nothing received yet, primes the meter at 0
	noisy.measure(1.0)
	coded := hammingEncode([]int{1, 0, 1, 1})
	coded[2] ^= 1 // one channel error the code will repair
	noisy.raw = coded
	noisy.render(fec, false)
	noisy.measure(2.0)
	if noisy.corrected != 1 {
		t.Errorf("corrected = %d, want 1", noisy.corrected)
	}
	if noisy.corrRate <= 0 {
		t.Errorf("repair rate = %g, want > 0", noisy.corrRate)
	}
}
