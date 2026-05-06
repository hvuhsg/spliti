package app_test

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

type cmpA struct{ V int }
type cmpB struct{ V int }

func TestQueryChanged_DetectsAddedEntities(t *testing.T) {
	a := app.New()
	app.TrackChanges[cmpA](a)

	var seen []int
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		// Spawn three entities with cmpA. Each addition should fire the
		// arche listener and land in the change buffer.
		m := generic.NewMap1[cmpA](c.World())
		for i := 1; i <= 3; i++ {
			m.NewWith(&cmpA{V: i})
		}
	})
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		app.QueryChanged1[cmpA](c, func(_ ecs.Entity, ca *cmpA) {
			seen = append(seen, ca.V)
		})
	})
	a.SetMaxFrames(1).Run()

	if len(seen) != 3 {
		t.Fatalf("got %d changed, want 3 (values=%v)", len(seen), seen)
	}
}

func TestQueryChanged_DrainsAtFrameEnd(t *testing.T) {
	a := app.New()
	app.TrackChanges[cmpA](a)

	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap1[cmpA](c.World())
		m.NewWith(&cmpA{V: 1})
	})

	frameSeen := make([]int, 0, 4)
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		var n int
		app.QueryChanged1[cmpA](c, func(_ ecs.Entity, _ *cmpA) { n++ })
		frameSeen = append(frameSeen, n)
	})
	a.SetMaxFrames(3).Run()

	// Frame 0: Startup added one entity → 1.
	// Frames 1, 2: nothing added, no MarkChanged → 0.
	want := []int{1, 0, 0}
	if len(frameSeen) != len(want) {
		t.Fatalf("got %v, want %v", frameSeen, want)
	}
	for i := range want {
		if frameSeen[i] != want[i] {
			t.Fatalf("frame %d: got %d, want %d (full=%v)", i, frameSeen[i], want[i], frameSeen)
		}
	}
}

func TestMarkChanged_AppendsToBuffer(t *testing.T) {
	a := app.New()
	app.TrackChanges[cmpA](a)

	var spawned ecs.Entity
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap1[cmpA](c.World())
		spawned = m.NewWith(&cmpA{V: 10})
	})

	type tickResult struct {
		frame int
		seen  []int
	}
	var results []tickResult

	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		// On frame 1, mutate via a Query and explicitly mark dirty.
		if c.App().Frame() == 1 {
			m := generic.NewMap1[cmpA](c.World())
			ca := m.Get(spawned)
			ca.V = 20
			app.MarkChanged[cmpA](c, spawned)
		}
		var seen []int
		app.QueryChanged1[cmpA](c, func(_ ecs.Entity, ca *cmpA) {
			seen = append(seen, ca.V)
		})
		results = append(results, tickResult{c.App().Frame(), seen})
	})
	a.SetMaxFrames(3).Run()

	// Frame 0: add detected (V=10).
	// Frame 1: MarkChanged → V=20 visible.
	// Frame 2: drained, nothing.
	if got := results[0].seen; len(got) != 1 || got[0] != 10 {
		t.Fatalf("frame 0: got %v, want [10]", got)
	}
	if got := results[1].seen; len(got) != 1 || got[0] != 20 {
		t.Fatalf("frame 1: got %v, want [20]", got)
	}
	if got := results[2].seen; len(got) != 0 {
		t.Fatalf("frame 2: got %v, want []", got)
	}
}

func TestQueryChanged_DedupesWithinFrame(t *testing.T) {
	a := app.New()
	app.TrackChanges[cmpA](a)

	var spawned ecs.Entity
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap1[cmpA](c.World())
		spawned = m.NewWith(&cmpA{V: 1})
	})
	var perFrame []int
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		if c.App().Frame() == 1 {
			// Mark the same entity many times — dedupe should keep it to one.
			for i := 0; i < 5; i++ {
				app.MarkChanged[cmpA](c, spawned)
			}
		}
		var n int
		app.QueryChanged1[cmpA](c, func(_ ecs.Entity, _ *cmpA) { n++ })
		perFrame = append(perFrame, n)
	})
	a.SetMaxFrames(2).Run()

	if perFrame[1] != 1 {
		t.Fatalf("frame 1 saw %d changed, want 1 (dedupe failed)", perFrame[1])
	}
}

func TestQueryChanged_PerTypeBuffersAreIndependent(t *testing.T) {
	a := app.New()
	app.TrackChanges[cmpA](a)
	app.TrackChanges[cmpB](a)

	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		ma := generic.NewMap1[cmpA](c.World())
		mb := generic.NewMap1[cmpB](c.World())
		ma.NewWith(&cmpA{V: 1})
		ma.NewWith(&cmpA{V: 2})
		mb.NewWith(&cmpB{V: 100})
	})
	var aN, bN int
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		app.QueryChanged1[cmpA](c, func(_ ecs.Entity, _ *cmpA) { aN++ })
		app.QueryChanged1[cmpB](c, func(_ ecs.Entity, _ *cmpB) { bN++ })
	})
	a.SetMaxFrames(1).Run()

	if aN != 2 || bN != 1 {
		t.Fatalf("got cmpA=%d cmpB=%d, want 2 and 1", aN, bN)
	}
}

func TestQueryChanged_PanicsWhenNotEnabled(t *testing.T) {
	a := app.New()
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("expected panic when QueryChanged1 is called without TrackChanges")
			}
		}()
		app.QueryChanged1[cmpA](c, func(ecs.Entity, *cmpA) {})
	})
	a.SetMaxFrames(1).Run()
}

func TestQueryChanged_SkipsDespawnedEntities(t *testing.T) {
	a := app.New()
	app.TrackChanges[cmpA](a)

	var spawned ecs.Entity
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap1[cmpA](c.World())
		spawned = m.NewWith(&cmpA{V: 7})
		// Despawn immediately via direct world mutation. The listener fired
		// on add (entity in buffer); the next QueryChanged should still
		// skip it gracefully because the entity is dead.
		c.World().RemoveEntity(spawned)
	})
	var visited int
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		app.QueryChanged1[cmpA](c, func(ecs.Entity, *cmpA) { visited++ })
	})
	a.SetMaxFrames(1).Run()

	if visited != 0 {
		t.Fatalf("visited %d, want 0 (despawned entity should be skipped)", visited)
	}
}
