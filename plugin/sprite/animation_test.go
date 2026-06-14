package sprite

import (
	"testing"
	gotime "time"
)

func walkClip(loop bool) map[string]*Clip {
	return map[string]*Clip{
		"walk": {Frames: []string{"a", "b", "c"}, DefaultDur: 100 * gotime.Millisecond, Loop: loop},
	}
}

func TestAdvanceProgression(t *testing.T) {
	a := &SpriteAnimation{Clips: walkClip(true), Active: "walk", Playing: true}

	// Below one frame duration: stays on frame 0.
	if ref, ok := a.Advance(60 * gotime.Millisecond); !ok || ref != "a" || a.Frame != 0 {
		t.Fatalf("after 60ms: ref=%q ok=%v frame=%d, want a/true/0", ref, ok, a.Frame)
	}
	// Crossing 100ms total advances to frame 1.
	if ref, _ := a.Advance(60 * gotime.Millisecond); ref != "b" || a.Frame != 1 {
		t.Fatalf("after 120ms: ref=%q frame=%d, want b/1", ref, a.Frame)
	}
	if ref, _ := a.Advance(100 * gotime.Millisecond); ref != "c" || a.Frame != 2 {
		t.Fatalf("after 220ms: ref=%q frame=%d, want c/2", ref, a.Frame)
	}
}

func TestAdvanceLoops(t *testing.T) {
	a := &SpriteAnimation{Clips: walkClip(true), Active: "walk", Playing: true}
	a.Frame = 2
	a.Advance(100 * gotime.Millisecond)
	if a.Frame != 0 {
		t.Fatalf("loop wrap: frame=%d, want 0", a.Frame)
	}
	if a.Finished {
		t.Error("looping clip should never be Finished")
	}
}

func TestAdvanceOneShotClamp(t *testing.T) {
	a := &SpriteAnimation{Clips: walkClip(false), Active: "walk", Playing: true}
	a.Frame = 2
	if ref, _ := a.Advance(100 * gotime.Millisecond); ref != "c" || a.Frame != 2 {
		t.Fatalf("one-shot end: ref=%q frame=%d, want c/2", ref, a.Frame)
	}
	if !a.Finished {
		t.Fatal("non-looping clip should be Finished at the end")
	}
	// Further advances hold the last frame and stay finished.
	if ref, ok := a.Advance(500 * gotime.Millisecond); ref != "c" || !ok || a.Frame != 2 || !a.Finished {
		t.Fatalf("after finish: ref=%q ok=%v frame=%d finished=%v", ref, ok, a.Frame, a.Finished)
	}
}

func TestAdvanceMultiFrameCatchUp(t *testing.T) {
	a := &SpriteAnimation{Clips: walkClip(true), Active: "walk", Playing: true}
	// 350ms over 100ms frames = 3 whole frames: 0 -> 1 -> 2 -> 0 (wrap).
	a.Advance(350 * gotime.Millisecond)
	if a.Frame != 0 {
		t.Fatalf("catch-up frame=%d, want 0 (wrapped)", a.Frame)
	}
}

func TestAdvancePerFrameDurations(t *testing.T) {
	a := &SpriteAnimation{
		Clips:   map[string]*Clip{"x": {Frames: []string{"a", "b"}, FrameDur: []gotime.Duration{50 * gotime.Millisecond, 200 * gotime.Millisecond}, DefaultDur: 100 * gotime.Millisecond, Loop: true}},
		Active:  "x",
		Playing: true,
	}
	// 60ms passes frame 0's 50ms budget -> frame 1.
	a.Advance(60 * gotime.Millisecond)
	if a.Frame != 1 {
		t.Fatalf("frame=%d, want 1", a.Frame)
	}
	// frame 1 needs 200ms; 60ms leftover (10ms) + 100ms not enough yet.
	a.Advance(100 * gotime.Millisecond)
	if a.Frame != 1 {
		t.Fatalf("frame=%d, want still 1", a.Frame)
	}
}

func TestPlayIdempotent(t *testing.T) {
	a := &SpriteAnimation{Clips: walkClip(true)}
	a.Play("walk")
	a.Advance(150 * gotime.Millisecond) // now on frame 1
	if a.Frame != 1 {
		t.Fatalf("setup frame=%d, want 1", a.Frame)
	}
	a.Play("walk") // already active+playing: must NOT reset
	if a.Frame != 1 {
		t.Errorf("Play of active clip reset frame to %d", a.Frame)
	}
	a.Play("walk_other") // unknown but different name resets
	if a.Frame != 0 || a.Active != "walk_other" {
		t.Errorf("Play of new clip: frame=%d active=%q", a.Frame, a.Active)
	}
}

func TestAdvancePaused(t *testing.T) {
	a := &SpriteAnimation{Clips: walkClip(true), Active: "walk", Playing: true}
	a.Pause()
	if ref, ok := a.Advance(500 * gotime.Millisecond); !ok || ref != "a" || a.Frame != 0 {
		t.Fatalf("paused: ref=%q ok=%v frame=%d, want a/true/0", ref, ok, a.Frame)
	}
}

func TestAdvanceNoClip(t *testing.T) {
	a := &SpriteAnimation{}
	if ref, ok := a.Advance(gotime.Second); ok || ref != "" {
		t.Fatalf("no clip: ref=%q ok=%v, want empty/false", ref, ok)
	}
}
