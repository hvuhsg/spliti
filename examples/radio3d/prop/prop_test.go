package prop

import (
	"math"
	"math/cmplx"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// --- geometry: mirror involution & point-in-rect ---

func TestMirrorInvolution(t *testing.T) {
	f := NewFace(m.Vec3{X: -1, Z: -1}, m.Vec3{X: 2}, m.Vec3{Z: 2}) // y=0 ground, normal +Y
	p := m.Vec3{X: 0.3, Y: 2.5, Z: -0.4}
	once := mirror(p, f)
	// Reflected across y=0 plane: y flips sign.
	if math.Abs(float64(once.Y+2.5)) > 1e-5 {
		t.Errorf("mirror y = %v, want -2.5", once.Y)
	}
	twice := mirror(once, f)
	if d := twice.Sub(p).Length(); d > 1e-5 {
		t.Errorf("mirror not involutive: off by %v", d)
	}
}

func TestPointInRect(t *testing.T) {
	f := NewFace(m.Vec3{}, m.Vec3{X: 4}, m.Vec3{Y: 2}) // rectangle in z=0
	if !pointInRect(m.Vec3{X: 2, Y: 1}, f) {
		t.Error("center should be inside")
	}
	if pointInRect(m.Vec3{X: 5, Y: 1}, f) {
		t.Error("x=5 should be outside")
	}
	if pointInRect(m.Vec3{X: 2, Y: -0.5}, f) {
		t.Error("y=-0.5 should be outside")
	}
}

// --- ray/face ---

func TestRayFace(t *testing.T) {
	f := NewFace(m.Vec3{X: -1, Z: -1}, m.Vec3{X: 2}, m.Vec3{Z: 2}) // y=0 plane
	tHit, pt, ok := rayFace(m.Vec3{Y: 3}, m.Vec3{Y: -1}, f)
	if !ok {
		t.Fatal("expected ray/face hit")
	}
	if math.Abs(float64(tHit-3)) > 1e-5 {
		t.Errorf("t = %v, want 3", tHit)
	}
	if pt.Length() > 1e-5 {
		t.Errorf("hit point = %+v, want origin", pt)
	}
}

// --- line of sight occlusion behind a box ---

func TestLineOfSightOcclusion(t *testing.T) {
	box := Box(m.Vec3{X: -1, Y: 0, Z: -1}, m.Vec3{X: 1, Y: 3, Z: 1})
	a := m.Vec3{X: -5, Y: 1.5}
	b := m.Vec3{X: 5, Y: 1.5}
	if LineOfSight(a, b, box, nil) {
		t.Error("segment through the box should be blocked")
	}
	// A segment passing above the box is clear.
	if !LineOfSight(m.Vec3{X: -5, Y: 5}, m.Vec3{X: 5, Y: 5}, box, nil) {
		t.Error("segment above the box should be clear")
	}
	// BVH path must agree with brute force.
	bvh := BuildBVH(box)
	if LineOfSight(a, b, box, bvh) {
		t.Error("BVH: segment through the box should be blocked")
	}
	if !LineOfSight(m.Vec3{X: -5, Y: 5}, m.Vec3{X: 5, Y: 5}, box, bvh) {
		t.Error("BVH: segment above the box should be clear")
	}
}

// --- free-space decay ~ 1/d^2 ---

func TestFreeSpaceDecay(t *testing.T) {
	tx := Tx{Pos: m.Vec3{}, PowerW: 1, FreqHz: 2.4e9}
	cfg := Config{MaxOrder: 0}
	pr := func(d float32) float64 {
		paths := Propagate(tx, m.Vec3{X: d}, nil, nil, cfg)
		pw, _ := Received(paths)
		return pw
	}
	p10 := pr(10)
	p20 := pr(20)
	// Doubling distance quarters the power (within tight tolerance).
	ratio := p10 / p20
	if ratio < 3.9 || ratio > 4.1 {
		t.Errorf("power ratio at d and 2d = %v, want ~4", ratio)
	}
}

// --- two-ray ground reflection: closed-form check ---

// The classic two-ray model: a transmitter and receiver at heights ht, hr,
// separated horizontally by d, over a flat ground with reflection coefficient Γ.
// The received field is the coherent sum of the direct ray (length d1) and the
// ground-reflected ray (length d2), and the power is |a_direct + a_reflected|².
func TestTwoRayGroundReflection(t *testing.T) {
	const ht, hr float32 = 5, 3
	const d float32 = 50
	freq := 9e8
	tx := Tx{Pos: m.Vec3{X: 0, Y: ht}, PowerW: 1, FreqHz: freq}
	rx := m.Vec3{X: d, Y: hr}

	// Large ground face in the y=0 plane spanning the geometry, normal +Y.
	ground := []Face{NewFace(m.Vec3{X: -10, Z: -10}, m.Vec3{X: 100}, m.Vec3{Z: 20})}
	bvh := BuildBVH(ground)
	cfg := Config{MaxOrder: 1, Reflection: -1}

	paths := Propagate(tx, rx, ground, bvh, cfg)
	if len(paths) != 2 {
		t.Fatalf("expected direct + ground reflection (2 paths), got %d", len(paths))
	}
	pw, _ := Received(paths)

	// Closed form.
	lambda := SpeedOfLight / freq
	k := 2 * math.Pi / lambda
	d1 := math.Hypot(float64(d), float64(ht-hr)) // direct
	d2 := math.Hypot(float64(d), float64(ht+hr)) // reflected (image at -ht)
	a1 := complex(lambda/(4*math.Pi*d1), 0) * cmplx.Exp(complex(0, -k*d1))
	a2 := complex(-1*lambda/(4*math.Pi*d2), 0) * cmplx.Exp(complex(0, -k*d2))
	want := cmplx.Abs(a1 + a2)
	wantPow := want * want

	if rel := math.Abs(pw-wantPow) / wantPow; rel > 1e-3 {
		t.Errorf("two-ray power = %v, want %v (rel err %v)", pw, wantPow, rel)
	}
}

// --- channel tap delays equal path length / c ---

func TestChannelTapDelays(t *testing.T) {
	tx := Tx{Pos: m.Vec3{}, PowerW: 1, FreqHz: 1e9}
	rx := m.Vec3{X: 30}
	paths := Propagate(tx, rx, nil, nil, Config{MaxOrder: 0})
	_, taps := Received(paths)
	if len(taps) != 1 {
		t.Fatalf("expected 1 tap, got %d", len(taps))
	}
	wantDelay := 30.0 / SpeedOfLight
	if math.Abs(float64(taps[0].Delay)-wantDelay) > 1e-12 {
		t.Errorf("delay = %v, want %v", taps[0].Delay, wantDelay)
	}
}

// --- coverage: shadow behind a building ---

func TestCoverageShadow(t *testing.T) {
	tx := Tx{Pos: m.Vec3{X: -20, Y: 2}, PowerW: 1, FreqHz: 2.4e9}
	box := Box(m.Vec3{X: -2, Y: 0, Z: -4}, m.Vec3{X: 2, Y: 10, Z: 4})
	bvh := BuildBVH(box)
	grid := Grid{MinX: -25, MaxX: 25, MinZ: -1, MaxZ: 1, NX: 51, NZ: 1, Y: 2}
	field := Coverage(tx, grid, box, bvh, Config{MaxOrder: 1, Reflection: -0.7})

	// A cell directly behind the box (shadowed) should receive less than a cell
	// the same distance away but with clear line of sight on the Tx side.
	behind := field[grid.Index(38, 0)] // x ~ +12.7, behind the box from Tx at -20
	front := field[grid.Index(12, 0)]  // x ~ -12.7, in front, clear LOS
	if behind >= front {
		t.Errorf("shadowed cell power %v should be < lit cell power %v", behind, front)
	}
}
