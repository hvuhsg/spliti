package panels

import (
	"fmt"

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

// Hierarchy lists every entity carrying an EditorMeta component, in
// world iteration order. Click a row to select that entity; arrow keys
// move selection up/down when this panel is focused.
//
// Performance: rebuilds its display list every frame. Spliti scenes are
// small enough (dozens of entities) that the alternative — diff-tracked
// state — would just be more code.
type Hierarchy struct {
	scroll int
}

// Name implements ui.Panel.
func (h *Hierarchy) Name() string { return ui.NameHierarchy }

// Title implements ui.Panel.
func (h *Hierarchy) Title() string { return "Hierarchy" }

// hierarchyEntry caches the per-entity display data for one frame.
type hierarchyEntry struct {
	entity ecs.Entity
	label  string
}

func (h *Hierarchy) collect(c *app.Ctx) []hierarchyEntry {
	world := c.World()
	tagID := ecs.ComponentID[runtime.Tag](world)
	tagMap := generic.NewMap1[runtime.Tag](world)
	var out []hierarchyEntry
	app.Query1[state.EditorMeta](c, func(e ecs.Entity, m *state.EditorMeta) {
		label := m.SceneName
		if world.Has(e, tagID) {
			if t := tagMap.Get(e); t != nil && t.Name != "" {
				label = t.Name
			}
		}
		if label == "" {
			label = fmt.Sprintf("entity_%d", e.ID())
		}
		out = append(out, hierarchyEntry{entity: e, label: label})
	})
	return out
}

// Render implements ui.Panel.
func (h *Hierarchy) Render(c *app.Ctx, r ui.Rect, focused bool) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	ui.DrawFill(s, r, ' ', tcell.StyleDefault)
	ui.DrawBox(s, r, h.Title(), focused)

	inner := r.Inset(1)
	if inner.W <= 0 || inner.H <= 0 {
		return
	}

	entries := h.collect(c)
	sel := app.GetResource[state.Selection](c)

	for i, e := range entries {
		row := i - h.scroll
		if row < 0 {
			continue
		}
		if row >= inner.H {
			break
		}
		style := ui.StyleText
		if sel != nil && sel.Active && sel.Entity == e.entity {
			style = ui.StyleSelected
			ui.DrawHLine(s, inner.X, inner.Y+row, inner.W, ' ', style)
		}
		ui.DrawTextClipped(s, inner.X, inner.Y+row, inner.W, style, e.label)
	}

	if len(entries) == 0 {
		ui.DrawTextClipped(s, inner.X, inner.Y, inner.W, ui.StyleTextDim, "(no entities)")
	}
}

// OnMouse implements ui.Panel. Click selects the entity on that row.
func (h *Hierarchy) OnMouse(c *app.Ctx, lx, ly int, ev input.MouseEvent) {
	if ev.Buttons&tcell.Button1 == 0 {
		return
	}
	// (lx,ly) is local to the panel rect; we need to subtract the inset.
	row := ly - 1 // top border = 1
	if row < 0 {
		return
	}
	row += h.scroll
	entries := h.collect(c)
	if row >= len(entries) {
		return
	}
	sel := app.GetResource[state.Selection](c)
	if sel == nil {
		return
	}
	sel.Entity = entries[row].entity
	sel.Active = true
}

// OnKey implements ui.Panel. Up/Down arrows move the selection through
// the visible entities; PageUp/PageDown scroll.
func (h *Hierarchy) OnKey(c *app.Ctx, ev input.KeyEvent) bool {
	entries := h.collect(c)
	if len(entries) == 0 {
		return false
	}
	sel := app.GetResource[state.Selection](c)
	if sel == nil {
		return false
	}
	idx := -1
	for i, e := range entries {
		if e.entity == sel.Entity {
			idx = i
			break
		}
	}
	switch ev.Key {
	case tcell.KeyUp:
		if idx <= 0 {
			idx = 0
		} else {
			idx--
		}
	case tcell.KeyDown:
		if idx == -1 {
			idx = 0
		} else if idx < len(entries)-1 {
			idx++
		}
	default:
		// 'n' triggers new-entity creation. Implemented through a
		// resource-flag because the panel doesn't depend on the top-
		// level editor package.
		if ev.Rune == 'n' {
			req := app.GetResource[NewEntityRequest](c)
			if req != nil {
				req.Pending = true
			}
			return true
		}
		// Delete selected entity with the 'd' key.
		if ev.Rune == 'd' {
			req := app.GetResource[DeleteEntityRequest](c)
			if req != nil {
				req.Entity = sel.Entity
			}
			return true
		}
		return false
	}
	sel.Entity = entries[idx].entity
	sel.Active = true
	return true
}

// NewEntityRequest is the per-frame signal from the hierarchy panel to
// the editor's create-entity handler. Set by the panel's 'n'
// shortcut; consumed by an editor system that calls CreateNewEntity.
type NewEntityRequest struct {
	Pending bool
}

// DeleteEntityRequest signals "remove this entity from the world" —
// the 'd' shortcut on the hierarchy panel writes it; an editor system
// performs the despawn through the Commands queue.
type DeleteEntityRequest struct {
	Entity ecs.Entity
}
