package panels_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/hvuhsg/spliti/editor/ui/panels"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/runtime"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// TestHierarchy_ArrowKeysMoveSelection verifies that pressing Down with
// the hierarchy panel focused advances the Selection through the
// EditorMeta-tagged entities in iteration order.
func TestHierarchy_ArrowKeysMoveSelection(t *testing.T) {
	a := app.New()
	app.InsertResource(a, &state.Selection{})
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		// Spawn 3 EditorMeta entities + Tag for stable display name.
		m := generic.NewMap2[state.EditorMeta, runtime.Tag](c.World())
		m.NewWith(&state.EditorMeta{SceneName: "a"}, &runtime.Tag{Name: "alpha"})
		m.NewWith(&state.EditorMeta{SceneName: "b"}, &runtime.Tag{Name: "bravo"})
		m.NewWith(&state.EditorMeta{SceneName: "c"}, &runtime.Tag{Name: "charlie"})
	})
	a.SetMaxFrames(1).Run()

	c := a.Ctx()
	h := &panels.Hierarchy{}

	// Initial: no selection. Down → selects first entity.
	if !h.OnKey(c, input.KeyEvent{Key: tcell.KeyDown}) {
		t.Fatal("OnKey down should be handled when entities exist")
	}
	sel := app.GetResource[state.Selection](c)
	if !sel.Active {
		t.Fatal("first Down should activate selection")
	}
	first := sel.Entity

	// Down → second entity, distinct from first.
	h.OnKey(c, input.KeyEvent{Key: tcell.KeyDown})
	if sel.Entity == first {
		t.Fatal("second Down should advance to a different entity")
	}

	// Up → back to first.
	h.OnKey(c, input.KeyEvent{Key: tcell.KeyUp})
	if sel.Entity != first {
		t.Fatal("Up should return to first entity")
	}

	// Two more Downs → third entity. A further Down should clamp.
	h.OnKey(c, input.KeyEvent{Key: tcell.KeyDown})
	h.OnKey(c, input.KeyEvent{Key: tcell.KeyDown})
	last := sel.Entity
	h.OnKey(c, input.KeyEvent{Key: tcell.KeyDown})
	if sel.Entity != last {
		t.Fatal("Down past end should clamp at last entity")
	}
	_ = ecs.Entity{}
}
