package panels

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/runtime"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Viewport is the center panel where the world renders. It does NOT draw
// the world itself — the tui plugin draws into the same rect via the
// Viewport resource (set by editor wiring code each frame). This panel
// only draws chrome (border + title) and routes mouse interactions
// (click-to-select, drag-to-move) into world coordinates.
type Viewport struct {
	// dragging tracks an in-progress drag started in this panel. When
	// non-zero, mouse-move events update the dragged entity's Position.
	dragging        ecs.Entity
	dragActive      bool
	dragOffsetX     int // world-coord offset from cursor to entity origin
	dragOffsetY     int
}

// Name implements ui.Panel.
func (v *Viewport) Name() string { return ui.NameViewport }

// Title implements ui.Panel.
func (v *Viewport) Title() string { return "Viewport" }

// Render implements ui.Panel. Draws only chrome — the world is drawn by
// the tui plugin's existing render system, clipped to this panel's rect.
func (v *Viewport) Render(c *app.Ctx, r ui.Rect, focused bool) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	mode := app.GetState[state.Mode](c)
	title := "Viewport"
	if mode != nil {
		title = "Viewport — " + mode.Get().String()
	}
	ui.DrawBox(s, r, title, focused)
}

// OnMouse implements ui.Panel. Click selects the entity under cursor;
// drag updates its Position. Coordinate conversion: panel-local → world
// requires subtracting the panel's interior origin (border = 1,1).
func (v *Viewport) OnMouse(c *app.Ctx, lx, ly int, ev input.MouseEvent) {
	wx, wy := lx-1, ly-1
	if wx < 0 || wy < 0 {
		return
	}
	pressed := ev.Buttons&tcell.Button1 != 0
	if !pressed {
		// Release — end any active drag.
		v.dragActive = false
		v.dragging = ecs.Entity{}
		return
	}
	sel := app.GetResource[state.Selection](c)
	if sel == nil {
		return
	}
	if !v.dragActive {
		// Start a press: hit-test for an entity at (wx,wy).
		hit := hitTest(c, wx, wy)
		if hit == (ecs.Entity{}) {
			sel.Active = false
			return
		}
		sel.Entity = hit
		sel.Active = true
		// Begin drag.
		posMap := generic.NewMap1[tui.Position](c.World())
		pos := posMap.Get(hit)
		v.dragActive = true
		v.dragging = hit
		v.dragOffsetX = pos.X - wx
		v.dragOffsetY = pos.Y - wy
		return
	}
	// Continue drag — move the entity to follow the cursor.
	if !c.World().Alive(v.dragging) {
		v.dragActive = false
		return
	}
	posMap := generic.NewMap1[tui.Position](c.World())
	pos := posMap.Get(v.dragging)
	pos.X = wx + v.dragOffsetX
	pos.Y = wy + v.dragOffsetY
}

// OnKey implements ui.Panel. The viewport itself has no keyboard
// shortcuts; in Play mode the runtime's KeyboardSystem reads events
// directly via input.KeyEvent so this panel's OnKey returning false
// lets global shortcuts (Esc=Pause, Space=Play/Pause) take over.
func (v *Viewport) OnKey(_ *app.Ctx, _ input.KeyEvent) bool { return false }

// hitTest finds an entity whose AABB or single-cell footprint covers
// world coords (wx,wy). Bounds-component entities are tried first
// (largest hit area wins by being checked); plain Position+Glyph
// entities match only their exact cell.
func hitTest(c *app.Ctx, wx, wy int) ecs.Entity {
	world := c.World()
	metaID := ecs.ComponentID[state.EditorMeta](world)
	var hit ecs.Entity
	app.Query2[tui.Position, runtime.Bounds](c, func(e ecs.Entity, p *tui.Position, b *runtime.Bounds) {
		if !world.Has(e, metaID) {
			return
		}
		if wx >= p.X && wx < p.X+b.W && wy >= p.Y && wy < p.Y+b.H {
			hit = e
		}
	})
	if hit != (ecs.Entity{}) {
		return hit
	}
	app.Query2[tui.Position, tui.Glyph](c, func(e ecs.Entity, p *tui.Position, _ *tui.Glyph) {
		if !world.Has(e, metaID) {
			return
		}
		if p.X == wx && p.Y == wy {
			hit = e
		}
	})
	return hit
}
