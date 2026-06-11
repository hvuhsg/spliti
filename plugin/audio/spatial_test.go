package audio

import (
	"math"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

func TestAttenuateLinear(t *testing.T) {
	o := SpatialOptions{}.withDefaults() // MinDist 1, MaxDist 50
	cases := []struct {
		dist, want float64
	}{
		{0, 1},
		{1, 1},                  // at MinDist: full volume
		{25.5, 0.5},             // midpoint
		{50, 0},                 // at MaxDist: silent
		{100, 0},                // beyond: still silent
	}
	for _, tc := range cases {
		if g := attenuate(tc.dist, o); math.Abs(g-tc.want) > 1e-9 {
			t.Errorf("linear dist %g: gain %g, want %g", tc.dist, g, tc.want)
		}
	}
}

func TestAttenuateInverse(t *testing.T) {
	o := SpatialOptions{MinDist: 2, MaxDist: 100, Rolloff: RolloffInverse}.withDefaults()
	if g := attenuate(1, o); g != 1 {
		t.Errorf("inside MinDist: gain %g, want 1", g)
	}
	if g := attenuate(4, o); math.Abs(g-0.5) > 1e-9 {
		t.Errorf("at 2×MinDist: gain %g, want 0.5", g)
	}
	if g := attenuate(100, o); g != 0 {
		t.Errorf("at MaxDist: gain %g, want 0 (taper)", g)
	}
	if g := attenuate(500, o); g != 0 {
		t.Errorf("beyond MaxDist: gain %g, want 0", g)
	}
	// Monotone non-increasing across the whole range, no pop at the taper.
	prev := math.Inf(1)
	for d := 0.0; d <= 110; d += 0.5 {
		g := attenuate(d, o)
		if g > prev+1e-9 {
			t.Fatalf("gain increased at dist %g: %g -> %g", d, prev, g)
		}
		prev = g
	}
}

func TestPanFromLateral(t *testing.T) {
	o := SpatialOptions{}.withDefaults()
	// Source straight to the right (lateral = dist): full right pan.
	if p := panFromLateral(10, 10, o); math.Abs(p-1) > 1e-9 {
		t.Errorf("hard right: pan %g, want 1", p)
	}
	// Straight left mirrors.
	if p := panFromLateral(-10, 10, o); math.Abs(p+1) > 1e-9 {
		t.Errorf("hard left: pan %g, want -1", p)
	}
	// Dead ahead: centered.
	if p := panFromLateral(0, 10, o); p != 0 {
		t.Errorf("ahead: pan %g, want 0", p)
	}
	// On top of the listener: centered, no flip.
	if p := panFromLateral(0, 0, o); p != 0 {
		t.Errorf("at listener: pan %g, want 0", p)
	}
	// Inside MinDist panning relaxes toward center (proximity factor).
	if p := panFromLateral(0.5, 0.5, o); math.Abs(p-0.5) > 1e-9 {
		t.Errorf("close-by: pan %g, want 0.5 (relaxed)", p)
	}
}

func TestSpatial2DVoice(t *testing.T) {
	a := NewAudio(testRate, 8)
	mustLoadPCM(t, a, "dc", constPCM(1, testRate), 1, testRate)
	a.SetListener2D(0, 0)

	// Source 10 right of the listener: panned right, attenuated by distance.
	sp := SpatialOptions{MinDist: 1, MaxDist: 21}
	h := a.PlayAt2D("dc", 10, 0, PlayOptions{Loop: true}, sp)
	l, r := readFrames(t, a, 64)
	if float64(l[10]) > 0.05 || float64(r[10]) < 0.3 {
		t.Fatalf("source to the right: got L=%g R=%g", l[10], r[10])
	}
	// Walk the listener onto the source: gain ramps to full, pan to center.
	a.SetListener2D(10, 0)
	readFrames(t, a, a.rampFrames()+64)
	l, r = readFrames(t, a, 16)
	if math.Abs(float64(l[0])-centerPan) > 1e-3 || math.Abs(float64(r[0])-centerPan) > 1e-3 {
		t.Fatalf("on top of source: got L=%g R=%g, want ~%g both", l[0], r[0], centerPan)
	}

	// Walk far beyond MaxDist: silent.
	a.SetListener2D(100, 0)
	readFrames(t, a, a.rampFrames()+64)
	l, _ = readFrames(t, a, 16)
	if l[0] != 0 {
		t.Fatalf("beyond MaxDist: got %g, want 0", l[0])
	}
	if !a.IsPlaying(h) {
		t.Fatal("inaudible spatial voice must keep playing")
	}

	// SetVoicePos2D moves the source back into earshot.
	a.SetVoicePos2D(h, 100, 0)
	readFrames(t, a, a.rampFrames()+64)
	l, _ = readFrames(t, a, 16)
	if l[0] == 0 {
		t.Fatal("voice moved onto listener is silent")
	}
}

func TestSpatial3DVoice(t *testing.T) {
	a := NewAudio(testRate, 8)
	mustLoadPCM(t, a, "dc", constPCM(1, testRate), 1, testRate)

	// render3d-style camera at origin looking down -Z, +Y up → +X is right.
	a.SetListener3D(m.Vec3{}, m.Vec3{Z: -1}, m.Vec3{Y: 1})
	sp := SpatialOptions{MinDist: 1, MaxDist: 100}

	right := a.PlayAt3D("dc", m.Vec3{X: 5}, PlayOptions{Loop: true}, sp)
	l, r := readFrames(t, a, 64)
	if float64(l[10]) > 0.05 || float64(r[10]) < 0.3 {
		t.Fatalf("source at +X: got L=%g R=%g, want hard right", l[10], r[10])
	}
	a.Stop(right)

	// Turn the camera around (+Z forward): +X is now the LEFT ear.
	a.SetListener3D(m.Vec3{}, m.Vec3{Z: 1}, m.Vec3{Y: 1})
	a.PlayAt3D("dc", m.Vec3{X: 5}, PlayOptions{Loop: true}, sp)
	readFrames(t, a, a.rampFrames()+64)
	l, r = readFrames(t, a, 16)
	if float64(r[0]) > 0.05 || float64(l[0]) < 0.3 {
		t.Fatalf("source at +X behind a turned camera: got L=%g R=%g, want hard left", l[0], r[0])
	}
}
