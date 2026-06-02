package editor_test

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor"
	"github.com/hvuhsg/spliti/editor/components"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/hvuhsg/spliti/plugin/runtime"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// TestSnapshot_RestoresPositionAfterMutation verifies the Play→Stop
// sequence: capture before mutating, mutate, restore, expect original.
func TestSnapshot_RestoresPositionAfterMutation(t *testing.T) {
	if len(components.Registry()) == 0 {
		components.RegisterBuiltins()
	}

	a := app.New()
	var snap *editor.PlaySnapshot

	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		w := c.World()
		mPos := generic.NewMap1[tui.Position](w)
		mMeta := generic.NewMap1[state.EditorMeta](w)
		mVel := generic.NewMap1[runtime.Velocity](w)

		e := w.NewEntity()
		mMeta.Add(e)
		*mMeta.Get(e) = state.EditorMeta{SceneName: "ball"}
		mPos.Add(e)
		*mPos.Get(e) = tui.Position{X: 10, Y: 5}
		mVel.Add(e)
		*mVel.Get(e) = runtime.Velocity{DX: 1, DY: 0}
	})

	// Capture snapshot, then mutate Position, then assert state is
	// post-mutation.
	a.AddSystems(schedule.Last, func(c *app.Ctx) {
		snap = editor.CaptureSnapshot(c)
		// Move the ball.
		app.Query2[tui.Position, state.EditorMeta](c, func(_ ecs.Entity, p *tui.Position, _ *state.EditorMeta) {
			p.X = 99
			p.Y = 99
		})
	})
	a.SetMaxFrames(1).Run()

	// Mid-state: ball should be at (99,99).
	app.Query2[tui.Position, state.EditorMeta](a.Ctx(), func(_ ecs.Entity, p *tui.Position, _ *state.EditorMeta) {
		if p.X != 99 || p.Y != 99 {
			t.Fatalf("pre-restore: pos = %+v, want {99,99}", p)
		}
	})

	// Restore in a fresh app frame so Commands flush properly.
	b := app.New()
	b.AddSystems(schedule.Startup, func(c *app.Ctx) {
		editor.RestoreSnapshot(c, snap)
	})
	b.SetMaxFrames(1).Run()

	count := 0
	app.Query2[tui.Position, state.EditorMeta](b.Ctx(), func(_ ecs.Entity, p *tui.Position, m *state.EditorMeta) {
		count++
		if p.X != 10 || p.Y != 5 {
			t.Fatalf("post-restore: pos = %+v, want {10,5}", p)
		}
		if m.SceneName != "ball" {
			t.Fatalf("post-restore: name = %q, want ball", m.SceneName)
		}
	})
	if count != 1 {
		t.Fatalf("expected 1 restored entity, got %d", count)
	}

	// Velocity should also have round-tripped.
	app.Query2[runtime.Velocity, state.EditorMeta](b.Ctx(), func(_ ecs.Entity, v *runtime.Velocity, _ *state.EditorMeta) {
		if v.DX != 1 || v.DY != 0 {
			t.Fatalf("post-restore: vel = %+v, want {1,0}", v)
		}
	})
}
