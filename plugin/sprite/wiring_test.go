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
// confirms it cycles Sprite.Ref through the clip's frames. Timing is loose (real
// wall clock), so the clip uses a 1ns frame duration and we assert the second
// frame was reached at some point rather than an exact index.
func TestAnimateSpritesWiring(t *testing.T) {
	a := app.New().AddPlugins(splititime.Plugin{FixedTimestep: gotime.Hour}, sprite.Plugin{})

	var e ecs.Entity
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap2[sprite.Sprite, sprite.SpriteAnimation](c.World())
		e = m.NewWith(&sprite.Sprite{Ref: "a"}, &sprite.SpriteAnimation{
			Clips:   map[string]*sprite.Clip{"walk": {Frames: []string{"a", "b"}, DefaultDur: gotime.Nanosecond, Loop: true}},
			Active:  "walk",
			Playing: true,
		})
	})

	seen := map[string]bool{}
	a.AddSystems(schedule.Last, func(c *app.Ctx) {
		m := generic.NewMap[sprite.Sprite](c.World())
		seen[m.Get(e).Ref] = true
	})

	a.SetMaxFrames(5).Run()

	if !seen["b"] {
		t.Fatalf("Sprite.Ref never advanced to frame \"b\"; saw %v", seen)
	}
}
