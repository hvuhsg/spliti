package sim

import (
	"math"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// insertionLossDB is the one-way transmission loss of a slab in decibels.
func insertionLossDB(g complex128) float64 { return -20 * math.Log10(cAbs(g)) }

// A perfect conductor transmits nothing.
func TestSlabPEC(t *testing.T) {
	metal := DefaultMaterialDB().Get(MatMetal)
	if g, _ := SlabTransmission(metal, 2.4e9, 0, TE); g != 0 {
		t.Errorf("PEC slab transmission = %v, want 0", g)
	}
}

// A lossless half-wave slab is a perfect transmission window: |T| = 1. For a
// lossless dielectric of index n at normal incidence, the slab phase φ = k₀·d·n;
// choosing d = λ/(2n) gives φ = π and |T| = 1 regardless of the impedance
// mismatch.
func TestSlabHalfWaveWindow(t *testing.T) {
	const f = 1e9
	const n = 2.0 // εr = 4, lossless
	lambda := SpeedOfLight / f
	d := lambda / (2 * n) // φ = π at normal incidence
	mat := Material{Name: "lossless", A: n * n, B: 0, C: 0, D: 0, ThicknessM: d}

	g, _ := SlabTransmission(mat, f, 0, TE)
	if math.Abs(cAbs(g)-1) > 1e-9 {
		t.Errorf("half-wave |T| = %v, want 1", cAbs(g))
	}
}

// Insertion loss grows with thickness for a lossy material, and a thick concrete
// wall attenuates far more than a thin glass pane at the same frequency.
func TestSlabLossMonotoneAndOrdering(t *testing.T) {
	db := DefaultMaterialDB()
	concrete := db.Get(MatConcrete)

	thin := concrete
	thin.ThicknessM = 0.1
	thick := concrete
	thick.ThicknessM = 0.4
	gThin, _ := SlabTransmission(thin, 2.4e9, 0, TE)
	gThick, _ := SlabTransmission(thick, 2.4e9, 0, TE)
	if insertionLossDB(gThick) <= insertionLossDB(gThin) {
		t.Errorf("thicker concrete should lose more: thin %.1f dB, thick %.1f dB",
			insertionLossDB(gThin), insertionLossDB(gThick))
	}

	glass := db.Get(MatGlass) // 6 mm
	gGlass, _ := SlabTransmission(glass, 2.4e9, 0, TE)
	gConcrete, _ := SlabTransmission(db.Get(MatConcrete), 2.4e9, 0, TE) // 0.30 m
	if insertionLossDB(gConcrete) <= insertionLossDB(gGlass) {
		t.Errorf("concrete (%.1f dB) should attenuate more than glass (%.1f dB)",
			insertionLossDB(gConcrete), insertionLossDB(gGlass))
	}
	// A glass pane should be fairly transparent (single-digit-ish dB), a concrete
	// wall clearly lossy.
	if insertionLossDB(gGlass) > 15 {
		t.Errorf("glass insertion loss %.1f dB unexpectedly high", insertionLossDB(gGlass))
	}
	if insertionLossDB(gConcrete) < 5 {
		t.Errorf("concrete insertion loss %.1f dB unexpectedly low", insertionLossDB(gConcrete))
	}
}

// With transmission enabled, a receiver behind a wall still receives a signal
// (attenuated), whereas with transmission disabled the blocked direct path
// vanishes. The wall here is a single concrete slab face spanning the gap.
func TestEngineTransmissionThroughWall(t *testing.T) {
	tx := Tx{Pos: m.Vec3{X: -5, Y: 1.5}, PowerW: 1, FreqHz: 2.4e9}
	rx := Rx{Pos: m.Vec3{X: 5, Y: 1.5}}

	// A vertical wall at x=0 spanning the segment, normal ±X, concrete.
	wall := NewFace(m.Vec3{X: 0, Y: -5, Z: -5}, m.Vec3{Y: 10}, m.Vec3{Z: 10})
	sc := NewScene([]Face{wall}) // defaults to concrete

	var eng Engine = ImageEngine{}

	blocked := eng.Paths(tx, rx, sc, Config{MaxOrder: 0, Transmission: false})
	if len(blocked) != 0 {
		t.Fatalf("without transmission the wall should block the direct path, got %d", len(blocked))
	}

	through := eng.Paths(tx, rx, sc, Config{MaxOrder: 0, Transmission: true})
	if len(through) != 1 {
		t.Fatalf("with transmission expected 1 attenuated path, got %d", len(through))
	}
	pwThrough, _ := Received(through)

	// Free-space (no wall) reference at the same distance.
	free := eng.Paths(tx, rx, NewScene(nil), Config{MaxOrder: 0})
	pwFree, _ := Received(free)

	if pwThrough <= 0 || pwThrough >= pwFree {
		t.Errorf("through-wall power %v should be positive and below free-space %v", pwThrough, pwFree)
	}
}
