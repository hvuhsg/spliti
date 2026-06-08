package main

import "testing"

// box is a 4×4-footprint wall centered at the origin, tall enough (top at 16 m, well
// above the 2.5 m antennas) to cast a real shadow. Used across the LoS tests.
var box = blockBox{cx: 0, cz: 0, hx: 2, hz: 2, topY: 16}

// lowFreq / highFreq bracket the lab's tunable band (10–45 MHz) — a ~30 m vs ~7 m
// wavelength, so the same obstacle shadows them very differently.
const (
	lowFreq  = 10e6
	highFreq = 45e6
)

// TestLoSBlockedAttenuates verifies a segment passing straight through a block is
// in shadow and attenuated below unity.
func TestLoSBlockedAttenuates(t *testing.T) {
	if !segHitsBox(-30, 0, 30, 0, box) {
		t.Fatal("segment through the box center should hit")
	}
	if got := losAmp(-30, 0, 30, 0, highFreq, []blockBox{box}); got >= 1 {
		t.Fatalf("blocked amplitude: got %g, want < 1", got)
	}
}

// TestLoSClear verifies a segment that misses the footprint is unattenuated.
func TestLoSClear(t *testing.T) {
	if segHitsBox(-30, 10, 30, 10, box) {
		t.Fatal("segment well clear of the box should miss")
	}
	if got := losAmp(-30, 10, 30, 10, highFreq, []blockBox{box}); got != 1 {
		t.Fatalf("clear amplitude: got %g, want 1", got)
	}
}

// TestLoSFrequencyDependence verifies the core physics the model exists for: a
// higher carrier (shorter wavelength) is shadowed more deeply than a lower one
// along the same obstructed path.
func TestLoSFrequencyDependence(t *testing.T) {
	lo := losAmp(-30, 0, 8, 0, lowFreq, []blockBox{box})
	hi := losAmp(-30, 0, 8, 0, highFreq, []blockBox{box})
	if !(hi < lo) {
		t.Fatalf("high carrier should be shadowed more: hi=%g, lo=%g", hi, lo)
	}
	if lo > 1 || hi < shadowFloor {
		t.Fatalf("factors out of range: lo=%g, hi=%g", lo, hi)
	}
}

// TestLoSTallerWallDarker verifies the shadow deepens with wall height: a taller
// wall forces a longer over-the-top detour and so casts a darker shadow than a short
// one on the same obstructed path.
func TestLoSTallerWallDarker(t *testing.T) {
	short := losAmp(-30, 0, 30, 0, lowFreq, []blockBox{{cx: 0, cz: 0, hx: 2, hz: 2, topY: markerHeight + 4}})
	tall := losAmp(-30, 0, 30, 0, lowFreq, []blockBox{{cx: 0, cz: 0, hx: 2, hz: 2, topY: markerHeight + 30}})
	if !(tall < short) {
		t.Fatalf("taller wall should shadow more: tall=%g, short=%g", tall, short)
	}
}

// TestLoSShortWallPassesOver verifies a wall no taller than the antennas casts no
// shadow — the wave clears its top without an appreciable detour.
func TestLoSShortWallPassesOver(t *testing.T) {
	low := blockBox{cx: 0, cz: 0, hx: 2, hz: 2, topY: markerHeight}
	if !segHitsBox(-30, 0, 30, 0, low) {
		t.Fatal("segment through the footprint should still register a hit")
	}
	if got := losAmp(-30, 0, 30, 0, highFreq, []blockBox{low}); got != 1 {
		t.Fatalf("a wall no taller than the antennas should not shadow: got %g, want 1", got)
	}
}

// TestLoSEndpointBeforeBlock verifies that a block beyond the segment's far
// endpoint does not occlude it — only obstacles actually between the pair count.
func TestLoSEndpointBeforeBlock(t *testing.T) {
	if segHitsBox(-30, 0, -10, 0, box) {
		t.Fatal("box past the segment's end should not hit")
	}
	if got := losAmp(-30, 0, -10, 0, highFreq, []blockBox{box}); got != 1 {
		t.Fatalf("path ending before the block should be clear: got %g", got)
	}
}

// TestLoSPowerIsAmplitudeSquared keeps the power/amplitude relationship the link
// budget relies on.
func TestLoSPowerIsAmplitudeSquared(t *testing.T) {
	a := losAmp(-30, 0, 30, 0, highFreq, []blockBox{box})
	p := losPower(-30, 0, 30, 0, highFreq, []blockBox{box})
	if d := p - a*a; d < -1e-12 || d > 1e-12 {
		t.Fatalf("power should equal amplitude²: power=%g, amp²=%g", p, a*a)
	}
}
