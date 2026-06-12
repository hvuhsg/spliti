package render3d

import (
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// TestFacingForwardRoundTrip checks that a transform aimed with Facing reports
// that same direction as its world forward axis — the single source of truth a
// directional light reads.
func TestFacingForwardRoundTrip(t *testing.T) {
	dirs := []m.Vec3{
		{X: -0.4, Y: -1, Z: -0.3},
		{Z: -1}, // already -Z (identity case)
		{Z: 1},  // antiparallel case
		{X: 1},  // sideways
		{Y: -1}, // straight down
		{X: 2, Y: 1, Z: -3},
	}
	for _, d := range dirs {
		tr := XForm().Facing(d)
		got := Forward(tr.matrix())
		want := d.Normalize()
		if got.Sub(want).Length() > 1e-5 {
			t.Errorf("Facing(%v): forward = %v, want %v", d, got, want)
		}
	}
}

// TestForwardIsNegZColumn confirms Forward reads the negated, normalized -Z
// column of the matrix (independent of translation and scale).
func TestForwardIsNegZColumn(t *testing.T) {
	tr := XForm().At(5, -2, 3).EulerDeg(-60, 30, 0).Scaled(2, 2, 2)
	got := Forward(tr.matrix())
	if l := got.Length(); l < 0.9999 || l > 1.0001 {
		t.Fatalf("Forward not unit length: %v (len %v)", got, l)
	}
	// EulerDeg(-60, 30, 0) aims down-and-sideways; Y must dominate downward.
	if got.Y > -0.5 {
		t.Fatalf("expected a downward-ish sun, got %v", got)
	}
}
