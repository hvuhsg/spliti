package main

import (
	"math"
	"math/cmplx"
	"testing"

	"github.com/hvuhsg/spliti/examples/radiosim/sim"
)

// wall is a long, thin reflector centered just above the TX/RX line: its near
// (-Z) face sits at z = 3 and mirrors a source below it. Both antennas sit at
// z = 0, on the reflective side of that face, so a single specular bounce exists.
var wall = blockBox{cx: 0, cz: 5, hx: 20, hz: 2, topY: 16}

// TestReflectionGeometry checks the image method against a hand-worked case: TX at
// (-10,0), RX at (10,0), reflecting off the wall's -Z face at z = 3. The image of
// TX is (-10,6), so the bounce lands at (0,3) and the path length is √436.
func TestReflectionGeometry(t *testing.T) {
	blocks := []blockBox{wall}
	faces := blockFaces(blocks)
	tx, rx := p2{x: -10, z: 0}, p2{x: 10, z: 0}
	nodes := buildImages(tx, faces, 1)

	paths := reflectionRays(tx, rx, markerHeight, 24e6, blocks, nodes, 0.6, 1)
	if len(paths) != 1 {
		t.Fatalf("expected exactly one order-1 reflection, got %d", len(paths))
	}
	p := paths[0]
	if p.order != 1 {
		t.Fatalf("order: got %d, want 1", p.order)
	}
	if len(p.pts) != 3 {
		t.Fatalf("polyline points: got %d, want 3 (tx, bounce, rx)", len(p.pts))
	}
	bx, bz := p.pts[1].X, p.pts[1].Z
	if math.Abs(float64(bx)-0) > 1e-4 || math.Abs(float64(bz)-3) > 1e-4 {
		t.Fatalf("bounce point: got (%g,%g), want (0,3)", bx, bz)
	}
	wantLen := math.Sqrt(436)
	if math.Abs(p.length-wantLen) > 1e-6 {
		t.Fatalf("path length: got %g, want %g", p.length, wantLen)
	}
	// No wall shadows the bounce legs (the reflector is excluded from its own
	// legs), so the only loss is the single reflection coefficient.
	if math.Abs(p.atten-0.6) > 1e-9 {
		t.Fatalf("attenuation: got %g, want 0.6 (one bounce, no shadowing)", p.atten)
	}
}

// TestTwoRayInterference verifies the core multipath effect: the direct and
// reflected copies add coherently, so the channel gain swings between near-sum and
// near-difference as the carrier sweeps the excess path length through a
// wavelength. Frequencies are chosen so the excess delay is exactly one wavelength
// (constructive) or half a wavelength (destructive).
func TestTwoRayInterference(t *testing.T) {
	blocks := []blockBox{wall}
	faces := blockFaces(blocks)
	tx, rx := p2{x: -10, z: 0}, p2{x: 10, z: 0}
	nodes := buildImages(tx, faces, 1)

	dDirect := dist(tx, rx)
	var taps []rfTap
	collectReflections(tx, rx, 24e6, blocks, nodes, 0.6, 1, &taps)
	if len(taps) != 1 {
		t.Fatalf("expected one reflected tap, got %d", len(taps))
	}
	excess := taps[0].length - dDirect

	// Relative reflected amplitude in the field model: atten · dDirect/length.
	a1 := taps[0].atten * dDirect / taps[0].length

	// gain(f) = |1 + a1·e^{−jk·excess}| with the direct path normalized to 1.
	gain := func(freq float64) float64 {
		k := 2 * math.Pi * freq / sim.SpeedOfLight
		h := complex(1, 0) + complex(a1, 0)*cmplx.Exp(complex(0, -k*excess))
		return cmplx.Abs(h)
	}

	fc := sim.SpeedOfLight / excess       // excess = 1λ → in phase
	fd := sim.SpeedOfLight / (2 * excess) // excess = ½λ → anti-phase
	gc, gd := gain(fc), gain(fd)

	if gc < 1+a1-1e-6 {
		t.Fatalf("constructive gain: got %g, want ≈ %g", gc, 1+a1)
	}
	if gd > 1-a1+1e-6 {
		t.Fatalf("destructive gain: got %g, want ≈ %g", gd, 1-a1)
	}
	if !(gc > gd) {
		t.Fatalf("constructive should exceed destructive: gc=%g, gd=%g", gc, gd)
	}
}

// TestNoReflectionWhenOneSided verifies the side test: with both antennas behind
// the wall (no face has them on its reflective side), the image method yields no
// valid path — multipath adds nothing, matching the direct-only behavior.
func TestNoReflectionWhenOneSided(t *testing.T) {
	// A small wall off to the side; TX and RX both far on the -X side of every
	// face that could face them, with the bounce geometry missing the finite span.
	b := blockBox{cx: 40, cz: 40, hx: 2, hz: 2, topY: 16}
	blocks := []blockBox{b}
	nodes := buildImages(p2{x: -10, z: 0}, blockFaces(blocks), 2)
	var taps []rfTap
	collectReflections(p2{x: -10, z: 0}, p2{x: 10, z: 0}, 24e6, blocks, nodes, 0.6, 2, &taps)
	if len(taps) != 0 {
		t.Fatalf("expected no valid reflection for a far off-axis wall, got %d", len(taps))
	}
}

// TestBuildImagesNoConsecutiveRepeat verifies the image enumeration never reflects
// off the same face twice in a row and produces the expected per-order counts.
func TestBuildImagesNoConsecutiveRepeat(t *testing.T) {
	blocks := []blockBox{{cx: 0, cz: 0, hx: 2, hz: 2, topY: 16}}
	faces := blockFaces(blocks) // 4 faces
	nodes := buildImages(p2{x: -10, z: 0}, faces, 2)
	// Order 1: 4 sequences. Order 2: 4·3 = 12 (no immediate repeat). Total 16.
	if len(nodes) != 16 {
		t.Fatalf("node count: got %d, want 16", len(nodes))
	}
	for _, n := range nodes {
		for i := 1; i < len(n.faces); i++ {
			if n.faces[i] == n.faces[i-1] {
				t.Fatalf("sequence reflects off the same face twice in a row: %+v", n.faces)
			}
		}
	}
}

// TestBuildImagesEmpty verifies that with no faces (or no walls) the image method
// yields nothing, so the multipath path collapses to direct-only.
func TestBuildImagesEmpty(t *testing.T) {
	if nodes := buildImages(p2{x: 0, z: 0}, nil, 2); nodes != nil {
		t.Fatalf("no faces should yield no images, got %d", len(nodes))
	}
}
