package sprite_test

import (
	"testing"
	gotime "time"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/sprite"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// TestAnimateSpritesWiring runs the real plugin system over a few frames and
// confirms it advances Sprite.Ref through the clip's frames.
//
// Timing is real wall clock, so the test is written to be insensitive to the
// exact per-frame delta. The clip is a one-shot (Loop: false) with a 1ns frame
// duration: once any nonzero delta arrives, Advance clamps on the last frame and
// holds it, so "b" is reached deterministically. (A looping 1ns clip would be
// flaky here — Advance fully drains Elapsed each tick, so the observed frame
// would just track the parity of the accumulated nanosecond delta, which is "a"
// whenever every per-frame delta happens to be even.) A small sleep per frame
// guarantees the wall clock advances even on a coarse/virtualized monotonic
// clock, so Delta() is reliably nonzero.
func TestAnimateSpritesWiring(t *testing.T) {
	a := app.New().AddPlugins(splititime.Plugin{FixedTimestep: gotime.Hour}, sprite.Plugin{})

	var e ecs.Entity
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap2[sprite.Sprite, sprite.SpriteAnimation](c.World())
		e = m.NewWith(&sprite.Sprite{Ref: "a"}, &sprite.SpriteAnimation{
			Clips:   map[string]*sprite.Clip{"walk": {Frames: []string{"a", "b"}, DefaultDur: gotime.Nanosecond, Loop: false}},
			Active:  "walk",
			Playing: true,
		})
	})

	seen := map[string]bool{}
	a.AddSystems(schedule.Last, func(c *app.Ctx) {
		m := generic.NewMap[sprite.Sprite](c.World())
		seen[m.Get(e).Ref] = true
		// Guarantee the wall clock advances before the next frame's Time tick so
		// Delta() is nonzero regardless of monotonic-clock resolution.
		gotime.Sleep(gotime.Millisecond)
	})

	a.SetMaxFrames(5).Run()

	if !seen["b"] {
		t.Fatalf("Sprite.Ref never advanced to frame \"b\"; saw %v", seen)
	}
}
