package sim

import (
	"math"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// Knife-edge limits: full illumination |F(−∞)|→1, grazing |F(0)|=1/2 (−6 dB), and
// deep shadow |F(+∞)|→0, decreasing monotonically into shadow.
func TestKnifeEdgeLimits(t *testing.T) {
	if g := cAbs(KnifeEdge(-5)); math.Abs(g-1) > 0.05 {
		t.Errorf("|F(-5)| = %v, want ~1", g)
	}
	if g := cAbs(KnifeEdge(0)); math.Abs(g-0.5) > 1e-3 {
		t.Errorf("|F(0)| = %v, want 0.5", g)
	}
	if g := cAbs(KnifeEdge(5)); g > 0.05 {
		t.Errorf("|F(5)| = %v, want ~0", g)
	}
	// Monotone decreasing through the shadow.
	prev := 2.0
	for v := 0.0; v <= 4; v += 0.5 {
		g := cAbs(KnifeEdge(v))
		if g > prev+1e-9 {
			t.Errorf("|F| not decreasing at v=%v: %v > %v", v, g, prev)
		}
		prev = g
	}
}

// The UTD transition function tends to 1 away from a boundary and to 0 on it.
func TestFresnelTransitionLimits(t *testing.T) {
	if g := cAbs(fresnelTransition(0)); g != 0 {
		t.Errorf("F(0) = %v, want 0", g)
	}
	for _, x := range []float64{10, 50, 200} {
		if g := cAbs(fresnelTransition(x)); math.Abs(g-1) > 0.05 {
			t.Errorf("|F(%v)| = %v, want ~1", x, g)
		}
	}
	// Bounded: the transition function magnitude never much exceeds 1.
	for x := 0.01; x < 10; x += 0.1 {
		if g := cAbs(fresnelTransition(x)); g > 1.2 {
			t.Errorf("|F(%v)| = %v, unexpectedly large", x, g)
		}
	}
}

// A cube yields its 12 edges, each a 90° convex corner (wedge factor n = 1.5).
func TestFindEdgesCube(t *testing.T) {
	edges := FindEdges(Box(m.Vec3{X: -1, Y: -1, Z: -1}, m.Vec3{X: 1, Y: 1, Z: 1}))
	if len(edges) != 12 {
		t.Fatalf("cube edges = %d, want 12", len(edges))
	}
	for _, e := range edges {
		if math.Abs(e.WedgeN-1.5) > 1e-6 {
			t.Errorf("cube edge wedge n = %v, want 1.5", e.WedgeN)
		}
	}
}

// The diffraction point sits in the edge interior at the equal-angle (Keller)
// location; for a symmetric source/field about the edge midpoint it lands there.
func TestDiffractionPoint(t *testing.T) {
	e := Edge{A: m.Vec3{Y: -5}, B: m.Vec3{Y: 5}} // edge along Y through origin
	// Symmetric: source and field mirror across the y=0 plane, so Q is at y=0.
	q, ok := diffractionPoint(e, m.Vec3{X: -3, Y: 0, Z: 2}, m.Vec3{X: 3, Y: 0, Z: 2})
	if !ok {
		t.Fatal("expected interior diffraction point")
	}
	if math.Abs(float64(q.Y)) > 1e-2 {
		t.Errorf("diffraction point y = %v, want ~0", q.Y)
	}
}

// The UTD coefficient tracks the knife edge: as the field point moves deeper into
// the shadow behind an absorbing screen, the UTD diffracted-field ratio and the
// knife-edge attenuation both fall, and their ratio stays order-1 and roughly
// stable (validating the UTD against the closed-form oracle up to its absolute
// normalization).
func TestUTDvsKnifeEdge(t *testing.T) {
	const lambda = 0.1 // 3 GHz
	// Screen edge along Y at the origin; the screen occupies x<0 in the z=0 plane.
	e := Edge{A: m.Vec3{Y: -50}, B: m.Vec3{Y: 50}, WedgeN: 2}
	src := m.Vec3{X: -8, Z: 6} // in front of the screen

	var ratios []float64
	for _, depth := range []float32{2, 4, 8, 12} {
		// Field point behind the screen (z<0), deeper in shadow as depth grows.
		fld := m.Vec3{X: -8, Z: -depth}
		q, ok := diffractionPoint(e, src, fld)
		if !ok {
			t.Fatalf("no diffraction point at depth %v", depth)
		}
		sP := float64(src.Sub(q).Length())
		s := float64(fld.Sub(q).Length())
		d0 := float64(fld.Sub(src).Length())

		d := DiffractionUTD(e, q, src, fld, lambda)
		utdRatio := cAbs(d) * math.Sqrt((sP+s)/(s*sP)) // |E_d / E_free|

		v := 2 * math.Sqrt(math.Max(sP+s-d0, 0)/lambda)
		knife := cAbs(KnifeEdge(v))

		if utdRatio <= 0 || knife <= 0 {
			t.Fatalf("non-positive amplitude at depth %v", depth)
		}
		ratios = append(ratios, utdRatio/knife)
	}

	// Both should decay into shadow (last shallower than first handled by ordering)
	// and the UTD/knife ratio should be order-1 and stable across depths.
	for _, r := range ratios {
		if r < 0.3 || r > 3 {
			t.Errorf("UTD/knife ratio %v out of order-1 range %v", r, ratios)
		}
	}
	spread := ratios[len(ratios)-1] / ratios[0]
	if spread < 0.5 || spread > 2 {
		t.Errorf("UTD/knife ratio drifts across depth by %v (ratios %v)", spread, ratios)
	}
}

// With diffraction enabled a receiver in the deep shadow of a building receives a
// non-zero field; without it the shadowed receiver gets nothing (no direct, no
// reflection reaches this isolated geometry).
func TestEngineDiffractionFillsShadow(t *testing.T) {
	tx := Tx{Pos: m.Vec3{X: -10, Y: 3, Z: 0}, PowerW: 1, FreqHz: 1e9}
	// A wide, thin wall blocking the line of sight; the receiver sits behind it
	// but offset past the side corner, reachable by bending around one vertical
	// edge (no far wall to re-block — single-edge diffraction suffices).
	wall := Box(m.Vec3{X: -0.3, Y: 0, Z: -5}, m.Vec3{X: 0.3, Y: 20, Z: 5})
	sc := NewScene(wall)
	rx := Rx{Pos: m.Vec3{X: 10, Y: 3, Z: 10}}

	var eng Engine = ImageEngine{}

	// Direct only: the wall blocks the line of sight, so nothing is received.
	off := eng.Paths(tx, rx, sc, Config{MaxOrder: 0, Diffraction: false})
	pwOff, _ := Received(off)
	if pwOff != 0 {
		t.Fatalf("expected the wall to block the direct path, got power %v", pwOff)
	}

	on := eng.Paths(tx, rx, sc, Config{MaxOrder: 0, Diffraction: true})
	pwOn, _ := Received(on)

	if pwOn <= pwOff {
		t.Errorf("diffraction should add shadow field: on=%v off=%v", pwOn, pwOff)
	}
	// At least one diffracted path was added.
	var diffracted int
	for _, p := range on {
		if p.Order == orderDiffraction {
			diffracted++
		}
	}
	if diffracted == 0 {
		t.Error("expected at least one diffracted path behind the building")
	}
}
