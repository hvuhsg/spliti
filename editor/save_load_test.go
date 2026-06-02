package editor_test

import (
	"path/filepath"
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

// TestScene_SaveLoadRoundTrip writes a curated scene, reloads it into
// a fresh world, and verifies entities + component values match.
func TestScene_SaveLoadRoundTrip(t *testing.T) {
	if len(components.Registry()) == 0 {
		components.RegisterBuiltins()
	}

	dir := t.TempDir()
	scenePath := filepath.Join(dir, "rt.scene")

	// Build a scene programmatically: 3 entities with mixed components.
	a := app.New()
	var emA, emB, emC ecs.Entity
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		w := c.World()
		mPos := generic.NewMap1[tui.Position](w)
		mMeta := generic.NewMap1[state.EditorMeta](w)
		mVel := generic.NewMap1[runtime.Velocity](w)
		mTag := generic.NewMap1[runtime.Tag](w)
		mBounds := generic.NewMap1[runtime.Bounds](w)

		emA = w.NewEntity()
		mMeta.Add(emA)
		*mMeta.Get(emA) = state.EditorMeta{SceneName: "a"}
		mPos.Add(emA)
		*mPos.Get(emA) = tui.Position{X: 1, Y: 2}
		mTag.Add(emA)
		mTag.Get(emA).Name = "alpha"

		emB = w.NewEntity()
		mMeta.Add(emB)
		*mMeta.Get(emB) = state.EditorMeta{SceneName: "b"}
		mPos.Add(emB)
		*mPos.Get(emB) = tui.Position{X: 5, Y: 6}
		mVel.Add(emB)
		*mVel.Get(emB) = runtime.Velocity{DX: 3, DY: -2}
		mBounds.Add(emB)
		*mBounds.Get(emB) = runtime.Bounds{W: 4, H: 5}

		emC = w.NewEntity()
		mMeta.Add(emC)
		*mMeta.Get(emC) = state.EditorMeta{SceneName: "c"}
		mPos.Add(emC)
		*mPos.Get(emC) = tui.Position{X: 0, Y: 0}
	})
	// Save in a different system so the spawn commands above are flushed first.
	a.AddSystems(schedule.Last, func(c *app.Ctx) {
		if err := editor.SaveScene(c, scenePath, "rt"); err != nil {
			t.Fatalf("save: %v", err)
		}
	})
	a.SetMaxFrames(1).Run()

	// Now load into a brand-new app and verify.
	b := app.New()
	b.AddSystems(schedule.Startup, func(c *app.Ctx) {
		if err := editor.LoadScene(c, scenePath); err != nil {
			t.Fatalf("load: %v", err)
		}
	})
	b.SetMaxFrames(1).Run()

	count := 0
	posCount, velCount, boundsCount, tagCount := 0, 0, 0, 0
	w := b.Ctx().World()
	posID := ecs.ComponentID[tui.Position](w)
	velID := ecs.ComponentID[runtime.Velocity](w)
	bID := ecs.ComponentID[runtime.Bounds](w)
	tID := ecs.ComponentID[runtime.Tag](w)
	posMap := generic.NewMap1[tui.Position](w)
	velMap := generic.NewMap1[runtime.Velocity](w)
	bMap := generic.NewMap1[runtime.Bounds](w)
	tMap := generic.NewMap1[runtime.Tag](w)
	app.Query1[state.EditorMeta](b.Ctx(), func(e ecs.Entity, meta *state.EditorMeta) {
		count++
		if w.Has(e, posID) {
			posCount++
			p := posMap.Get(e)
			switch meta.SceneName {
			case "a":
				if p.X != 1 || p.Y != 2 {
					t.Errorf("a position: %+v", p)
				}
			case "b":
				if p.X != 5 || p.Y != 6 {
					t.Errorf("b position: %+v", p)
				}
			case "c":
				if p.X != 0 || p.Y != 0 {
					t.Errorf("c position: %+v", p)
				}
			}
		}
		if w.Has(e, velID) {
			velCount++
			v := velMap.Get(e)
			if v.DX != 3 || v.DY != -2 {
				t.Errorf("b velocity: %+v", v)
			}
		}
		if w.Has(e, bID) {
			boundsCount++
			bb := bMap.Get(e)
			if bb.W != 4 || bb.H != 5 {
				t.Errorf("b bounds: %+v", bb)
			}
		}
		if w.Has(e, tID) {
			tagCount++
			if tg := tMap.Get(e).Name; tg != "alpha" {
				t.Errorf("a tag: %q", tg)
			}
		}
	})
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if posCount != 3 || velCount != 1 || boundsCount != 1 || tagCount != 1 {
		t.Fatalf("counts: pos=%d vel=%d bounds=%d tag=%d", posCount, velCount, boundsCount, tagCount)
	}
}
