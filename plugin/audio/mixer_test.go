package audio

import (
	"math"
	"testing"
	"time"
)

const testRate = 48000

// readFrames pulls n stereo frames out of the mixer and decodes them back to
// float32 pairs, exactly as the output device would see them.
func readFrames(t *testing.T, a *Audio, n int) (left, right []float32) {
	t.Helper()
	p := make([]byte, n*bytesPerFrame)
	got, err := a.Read(p)
	if err != nil || got != len(p) {
		t.Fatalf("Read = %d, %v; want %d, nil", got, err, len(p))
	}
	left = make([]float32, n)
	right = make([]float32, n)
	for i := 0; i < n; i++ {
		left[i] = math.Float32frombits(uint32(p[i*8]) | uint32(p[i*8+1])<<8 | uint32(p[i*8+2])<<16 | uint32(p[i*8+3])<<24)
		right[i] = math.Float32frombits(uint32(p[i*8+4]) | uint32(p[i*8+5])<<8 | uint32(p[i*8+6])<<16 | uint32(p[i*8+7])<<24)
	}
	return left, right
}

func constPCM(v float32, frames int) []float32 {
	pcm := make([]float32, frames)
	for i := range pcm {
		pcm[i] = v
	}
	return pcm
}

func mustLoadPCM(t *testing.T, a *Audio, ref string, pcm []float32, channels, rate int) {
	t.Helper()
	if err := a.Registry().LoadPCM(ref, pcm, channels, rate); err != nil {
		t.Fatalf("LoadPCM(%q): %v", ref, err)
	}
}

const centerPan = 0.70710678 // equal-power center gain, 1/sqrt(2)

func TestMixTwoBuffers(t *testing.T) {
	a := NewAudio(testRate, 8)
	mustLoadPCM(t, a, "a", constPCM(0.25, testRate), 1, testRate)
	mustLoadPCM(t, a, "b", constPCM(0.5, testRate), 1, testRate)

	a.PlayWith("a", PlayOptions{Loop: true})
	a.PlayWith("b", PlayOptions{Loop: true})
	a.SetBusVolume(BusSFX, 0.5)
	a.SetBusVolume(BusMaster, 0.5)

	// Read past the 8ms bus ramps, then check steady state.
	readFrames(t, a, testRate/100)
	l, r := readFrames(t, a, 64)
	want := float32((0.25 + 0.5) * 0.5 * 0.5 * centerPan)
	for i := range l {
		if math.Abs(float64(l[i]-want)) > 1e-5 || math.Abs(float64(r[i]-want)) > 1e-5 {
			t.Fatalf("frame %d = (%g, %g), want (%g, %g)", i, l[i], r[i], want, want)
		}
	}
}

func TestEqualPowerPan(t *testing.T) {
	for _, tc := range []struct {
		pan          float64
		wantL, wantR float64
	}{
		{-1, 1, 0},
		{0, centerPan, centerPan},
		{+1, 0, 1},
	} {
		a := NewAudio(testRate, 8)
		mustLoadPCM(t, a, "dc", constPCM(1, testRate), 1, testRate)
		a.PlayWith("dc", PlayOptions{Loop: true, Pan: tc.pan})
		l, r := readFrames(t, a, 64)
		if math.Abs(float64(l[10])-tc.wantL) > 1e-5 || math.Abs(float64(r[10])-tc.wantR) > 1e-5 {
			t.Errorf("pan %+.0f: got (%g, %g), want (%g, %g)", tc.pan, l[10], r[10], tc.wantL, tc.wantR)
		}
	}

	// Constant power across the sweep: L² + R² == 1 everywhere.
	for pan := -1.0; pan <= 1.0; pan += 0.125 {
		l, r := panGains(pan)
		if p := l*l + r*r; math.Abs(p-1) > 1e-9 {
			t.Errorf("pan %g: power %g, want 1", pan, p)
		}
	}
}

func TestLoopBoundaryContinuity(t *testing.T) {
	// A sine with whole periods in the buffer is continuous across the loop
	// seam; any seam bug (wrong lerp neighbor, a zero gap) shows up as a
	// sample-to-sample jump far above the wave's own slope.
	const n = 1000
	const periods = 10
	pcm := make([]float32, n)
	for i := range pcm {
		pcm[i] = float32(math.Sin(2 * math.Pi * periods * float64(i) / n))
	}
	a := NewAudio(testRate, 8)
	mustLoadPCM(t, a, "sine", pcm, 1, testRate)
	a.PlayWith("sine", PlayOptions{Loop: true, Pitch: 1.3})

	l, _ := readFrames(t, a, n*4) // several loop wraps at pitch 1.3
	maxSlope := 2 * math.Pi * periods / n * 1.3 * centerPan
	for i := 1; i < len(l); i++ {
		if d := math.Abs(float64(l[i] - l[i-1])); d > maxSlope*1.5 {
			t.Fatalf("discontinuity at frame %d: delta %g exceeds slope bound %g", i, d, maxSlope*1.5)
		}
	}
}

func TestHandleGenerationSafety(t *testing.T) {
	a := NewAudio(testRate, 1) // single slot forces reuse
	mustLoadPCM(t, a, "short", constPCM(0.5, 16), 1, testRate)
	mustLoadPCM(t, a, "long", constPCM(0.5, testRate), 1, testRate)

	old := a.Play("short")
	if !a.IsPlaying(old) {
		t.Fatal("fresh handle should be playing")
	}
	readFrames(t, a, 64) // drains the 16-frame one-shot
	if a.IsPlaying(old) {
		t.Fatal("finished voice still reports playing")
	}

	fresh := a.PlayWith("long", PlayOptions{Loop: true})
	if fresh.index() != old.index() {
		t.Fatalf("expected slot reuse, got %d vs %d", fresh.index(), old.index())
	}
	a.SetVolume(old, 0) // stale: must not touch the new voice
	a.Stop(old)         // stale: must not touch the new voice
	readFrames(t, a, testRate/100)
	l, _ := readFrames(t, a, 16)
	if l[0] == 0 {
		t.Fatal("stale handle affected the voice reusing its slot")
	}
	if !a.IsPlaying(fresh) || a.IsPlaying(old) {
		t.Fatalf("IsPlaying: fresh=%v old=%v, want true/false", a.IsPlaying(fresh), a.IsPlaying(old))
	}
}

func TestVolumeRampNoClick(t *testing.T) {
	a := NewAudio(testRate, 8)
	mustLoadPCM(t, a, "dc", constPCM(1, testRate), 1, testRate)
	h := a.PlayWith("dc", PlayOptions{Loop: true})
	readFrames(t, a, 100)

	a.SetVolume(h, 0)
	rampFrames := a.rampFrames()
	l, _ := readFrames(t, a, rampFrames+96)

	maxStep := 1.0/float64(rampFrames)*centerPan + 1e-6
	for i := 1; i < len(l); i++ {
		if l[i] > l[i-1]+1e-7 {
			t.Fatalf("ramp not monotone at frame %d: %g -> %g", i-1, l[i-1], l[i])
		}
		if d := float64(l[i-1] - l[i]); d > maxStep {
			t.Fatalf("click: step %g at frame %d exceeds ramp slope %g", d, i, maxStep)
		}
	}
	if tail := l[rampFrames+32]; tail != 0 {
		t.Fatalf("volume should reach 0 by frame %d, got %g", rampFrames+32, tail)
	}
}

func TestPitchResampleLength(t *testing.T) {
	const n = 4800
	a := NewAudio(testRate, 8)
	mustLoadPCM(t, a, "shot", constPCM(0.5, n), 1, testRate)

	fast := a.PlayWith("shot", PlayOptions{Pitch: 2})
	readFrames(t, a, n/2+64)
	if a.IsPlaying(fast) {
		t.Fatal("pitch 2.0 one-shot should finish after ~half its frames")
	}

	slow := a.PlayWith("shot", PlayOptions{Pitch: 0.5})
	readFrames(t, a, 2*n-64)
	if !a.IsPlaying(slow) {
		t.Fatal("pitch 0.5 one-shot ended early")
	}
	readFrames(t, a, 128)
	if a.IsPlaying(slow) {
		t.Fatal("pitch 0.5 one-shot should finish after ~double its frames")
	}
}

func TestLoadResamplePreservesDuration(t *testing.T) {
	// A 1-second asset at 24kHz must decode to ~1 second at the mixer rate.
	b, err := newBuffer(constPCM(0.5, 24000), 1, 24000, testRate)
	if err != nil {
		t.Fatal(err)
	}
	if b.Frames() != testRate {
		t.Fatalf("resampled frames = %d, want %d", b.Frames(), testRate)
	}
}

func TestClampManyVoices(t *testing.T) {
	a := NewAudio(testRate, 32)
	mustLoadPCM(t, a, "dc", constPCM(1, testRate), 1, testRate)
	for i := 0; i < 32; i++ {
		a.PlayWith("dc", PlayOptions{Loop: true})
	}
	l, r := readFrames(t, a, 256)
	for i := range l {
		if l[i] > 1 || l[i] < -1 || r[i] > 1 || r[i] < -1 {
			t.Fatalf("frame %d = (%g, %g) outside [-1, 1]", i, l[i], r[i])
		}
	}
	// 32 voices at center pan sum to ~22.6 pre-clamp; the clamp must engage.
	if l[100] != 1 {
		t.Fatalf("expected clamped output 1, got %g", l[100])
	}
}

func TestVoiceStealing(t *testing.T) {
	a := NewAudio(testRate, 2)
	mustLoadPCM(t, a, "dc", constPCM(1, testRate), 1, testRate)

	quiet := a.PlayWith("dc", PlayOptions{Loop: true, Volume: 0.1})
	loud := a.PlayWith("dc", PlayOptions{Loop: true, Volume: 0.9})
	third := a.PlayWith("dc", PlayOptions{Loop: true})
	if third == NoHandle {
		t.Fatal("expected the quiet voice to be stolen, got NoHandle")
	}
	if a.IsPlaying(quiet) {
		t.Fatal("quietest voice should have been stolen")
	}
	if !a.IsPlaying(loud) {
		t.Fatal("loud voice should have survived")
	}

	// A pool full of protected music voices refuses to steal.
	b := NewAudio(testRate, 1)
	mustLoadPCM(t, b, "dc", constPCM(1, testRate), 1, testRate)
	b.PlayMusic("dc", MusicOptions{})
	if h := b.Play("dc"); h != NoHandle {
		t.Fatal("stealing a protected voice")
	}
}

func TestPauseResume(t *testing.T) {
	a := NewAudio(testRate, 8)
	mustLoadPCM(t, a, "dc", constPCM(1, testRate), 1, testRate)
	h := a.PlayWith("dc", PlayOptions{Loop: true})
	readFrames(t, a, 100)

	a.Pause(h)
	readFrames(t, a, a.rampFrames()+64) // ramp out fully
	l, _ := readFrames(t, a, 64)
	if l[0] != 0 || l[63] != 0 {
		t.Fatalf("paused voice still audible: %g", l[0])
	}
	if !a.IsPlaying(h) {
		t.Fatal("paused voice should still be alive")
	}

	a.Resume(h)
	readFrames(t, a, a.rampFrames()+64)
	l, _ = readFrames(t, a, 16)
	if l[0] == 0 {
		t.Fatal("resumed voice is silent")
	}
}

func TestFinishedAndNullSinkAdvance(t *testing.T) {
	a := NewAudio(testRate, 8)
	mustLoadPCM(t, a, "half", constPCM(0.5, testRate/2), 1, testRate)
	h := a.Play("half")

	a.Advance(250 * time.Millisecond)
	if !a.IsPlaying(h) {
		t.Fatal("0.5s one-shot ended after 0.25s of Advance")
	}
	a.Advance(time.Second)
	if a.IsPlaying(h) {
		t.Fatal("0.5s one-shot still alive after 1.25s of Advance")
	}

	fin := a.drainFinished(nil)
	if len(fin) != 1 || fin[0].Handle != h || fin[0].Ref != "half" {
		t.Fatalf("drainFinished = %+v, want one event for %v/half", fin, h)
	}
	if fin := a.drainFinished(nil); len(fin) != 0 {
		t.Fatalf("second drain not empty: %+v", fin)
	}
}

func TestFadeToAndStopFade(t *testing.T) {
	a := NewAudio(testRate, 8)
	mustLoadPCM(t, a, "dc", constPCM(1, testRate), 1, testRate)
	h := a.PlayWith("dc", PlayOptions{Loop: true})

	a.FadeTo(h, 0, 50*time.Millisecond)
	a.Advance(100 * time.Millisecond)
	if !a.IsPlaying(h) {
		t.Fatal("FadeTo(0) must not free the voice")
	}
	l, _ := readFrames(t, a, 16)
	if l[0] != 0 {
		t.Fatalf("faded-to-zero voice audible: %g", l[0])
	}

	a.StopFade(h, 50*time.Millisecond)
	a.Advance(150 * time.Millisecond)
	if a.IsPlaying(h) {
		t.Fatal("StopFade must free the voice after the fade")
	}
}

func TestMusicCrossfade(t *testing.T) {
	a := NewAudio(testRate, 8)
	mustLoadPCM(t, a, "t1", constPCM(0.5, testRate), 1, testRate)
	mustLoadPCM(t, a, "t2", constPCM(0.5, testRate), 1, testRate)

	h1 := a.PlayMusic("t1", MusicOptions{})
	readFrames(t, a, 100)

	h2 := a.PlayMusic("t2", MusicOptions{Crossfade: 50 * time.Millisecond})
	if a.CurrentMusic() != h2 {
		t.Fatal("new track should be current immediately")
	}

	// During the crossfade both voices are live and the sum stays bounded by
	// a single track's level (complementary linear fades of equal signals).
	l, _ := readFrames(t, a, a.framesOf(25*time.Millisecond))
	if !a.IsPlaying(h1) || !a.IsPlaying(h2) {
		t.Fatal("both tracks should be live mid-crossfade")
	}
	bound := 0.5*centerPan + 1e-4
	for i, s := range l {
		if float64(s) > bound {
			t.Fatalf("crossfade sum %g at frame %d exceeds single-track level %g", s, i, bound)
		}
	}

	a.Advance(100 * time.Millisecond) // fade completes
	if a.IsPlaying(h1) {
		t.Fatal("old track should be freed when its fade lands")
	}
	if !a.IsPlaying(h2) || a.CurrentMusic() != h2 {
		t.Fatal("new track should keep playing at full level")
	}
}
