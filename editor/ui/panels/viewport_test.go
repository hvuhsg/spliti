package panels_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/hvuhsg/spliti/editor/ui/panels"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/runtime"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// TestViewport_DragMovesEntity simulates a click on an entity, then a
// move event with the button still held, and verifies the entity's
// Position followed the cursor.
func TestViewport_DragMovesEntity(t *testing.T) {
	a := app.New()
	app.InsertResource(a, &state.Selection{})

	var movable ecs.Entity
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		w := c.World()
		mPos := generic.NewMap1[tui.Position](w)
		mGlyph := generic.NewMap1[tui.Glyph](w)
		mMeta := generic.NewMap1[state.EditorMeta](w)
		mBounds := generic.NewMap1[runtime.Bounds](w)

		movable = w.NewEntity()
		mMeta.Add(movable)
		mPos.Add(movable)
		*mPos.Get(movable) = tui.Position{X: 5, Y: 5}
		mGlyph.Add(movable)
		mBounds.Add(movable)
		*mBounds.Get(movable) = runtime.Bounds{W: 1, H: 1}
	})
	a.SetMaxFrames(1).Run()

	v := &panels.Viewport{}
	c := a.Ctx()

	// Local coords: (5+1, 5+1) accounts for the panel's 1-cell border.
	press := input.MouseEvent{X: 6, Y: 6, Buttons: tcell.Button1}
	v.OnMouse(c, 6, 6, press)

	// Selection should be set.
	sel := app.GetResource[state.Selection](c)
	if !sel.Active || sel.Entity != movable {
		t.Fatalf("after press, sel = %+v want active+movable", sel)
	}

	// Drag: still holding button, move to local (10, 8) → world (9, 7).
	move := input.MouseEvent{X: 10, Y: 8, Buttons: tcell.Button1}
	v.OnMouse(c, 10, 8, move)

	posMap := generic.NewMap1[tui.Position](c.World())
	pos := posMap.Get(movable)
	if pos.X != 9 || pos.Y != 7 {
		t.Fatalf("after drag, pos = %+v want {9,7}", pos)
	}

	// Release ends the drag — further moves do nothing.
	release := input.MouseEvent{X: 10, Y: 8, Buttons: 0}
	v.OnMouse(c, 10, 8, release)
	stale := input.MouseEvent{X: 20, Y: 20, Buttons: 0}
	v.OnMouse(c, 20, 20, stale)
	if pos.X != 9 || pos.Y != 7 {
		t.Fatalf("post-release move shouldn't change pos: %+v", pos)
	}
}
