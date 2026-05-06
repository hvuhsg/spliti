// Package tui renders entities with Position + Glyph components to the
// terminal screen owned by the terminal plugin. Add tui.Plugin{} after
// terminal.Plugin{}.
package tui

import (
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/terminal"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Position places an entity in the terminal grid. (0,0) is the top-left cell.
type Position struct{ X, Y int }

// Glyph is the visual representation of an entity. Style controls fg/bg color
// and attributes; pass tcell.StyleDefault if you don't care.
type Glyph struct {
	Char  rune
	Style tcell.Style
}

// Layer overrides the draw order of a (Position, Glyph) entity. Higher Z draws
// on top of lower Z and on top of unlayered entities.
//
// Entities without a Layer render first via the unsorted fast path. Entities
// with a Layer are collected, stable-sorted by Z ascending, then drawn on top.
// If no entity in the world has a Layer component, the renderer skips the
// layered pass entirely — there is no per-frame allocation for the common
// case.
//
// Within a single Z value, draw order is arche's archetype-storage order
// (deterministic but not user-controlled). To put HUD glyphs above everything
// else, give them any positive Z.
type Layer struct{ Z int }

// renderLabel marks the entity-render system. Kept package-private so users
// extend the renderer through AddOverlay / AddPreRender instead of writing
// raw After/Before strings.
const (
	renderLabel  = "__spliti_render"
	presentLabel = "__spliti_present"
)

// layeredCell is a snapshot of a layered (Position, Glyph, Layer) entity for
// the per-frame sort. The render system reuses one buffer across frames.
type layeredCell struct {
	x, y  int
	ch    rune
	style tcell.Style
	z     int
}

// Plugin installs the render and present systems in PostUpdate.
//
// Render writes the back buffer (clear → entities); overlays write on top of
// it; present is the single per-frame Show that flushes everything to the
// terminal in one shot. Splitting draw from present is what prevents
// flicker — a Show between draw and overlay writes would briefly display a
// HUD-less frame.
type Plugin struct {
	// ClearStyle is the style used to clear the screen each frame. Zero
	// value uses tcell.StyleDefault.
	ClearStyle tcell.Style
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	clearStyle := p.ClearStyle

	// Reused across frames so the layered pass amortises its slice
	// allocation. Lives inside the closure — not a resource — because it's
	// implementation detail of this plugin's render system.
	layered := make([]layeredCell, 0, 32)
	unlayeredFilter := generic.NewFilter2[Position, Glyph]().Without(generic.T[Layer]())

	a.AddSystems(schedule.PostUpdate, app.System(func(c *app.Ctx) {
		term := app.GetResource[terminal.Terminal](c)
		if term == nil {
			return
		}
		s := term.Screen
		w, h := s.Size()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				s.SetContent(x, y, ' ', nil, clearStyle)
			}
		}

		// Fast path: unlayered entities. No allocation, no sort.
		uq := unlayeredFilter.Query(c.World())
		for uq.Next() {
			pos, g := uq.Get()
			if pos.X < 0 || pos.X >= w || pos.Y < 0 || pos.Y >= h {
				continue
			}
			s.SetContent(pos.X, pos.Y, g.Char, nil, g.Style)
		}

		// Layered pass: collect, stable-sort by Z, draw. Skipped entirely
		// when no entity has a Layer.
		layered = layered[:0]
		app.Query3[Position, Glyph, Layer](c, func(_ ecs.Entity, pos *Position, g *Glyph, l *Layer) {
			if pos.X < 0 || pos.X >= w || pos.Y < 0 || pos.Y >= h {
				return
			}
			layered = append(layered, layeredCell{x: pos.X, y: pos.Y, ch: g.Char, style: g.Style, z: l.Z})
		})
		if len(layered) > 0 {
			sort.SliceStable(layered, func(i, j int) bool { return layered[i].z < layered[j].z })
			for i := range layered {
				lc := &layered[i]
				s.SetContent(lc.x, lc.y, lc.ch, nil, lc.style)
			}
		}
	}).Label(renderLabel))

	a.AddSystems(schedule.PostUpdate, app.System(func(c *app.Ctx) {
		term := app.GetResource[terminal.Terminal](c)
		if term == nil {
			return
		}
		term.Screen.Show()
	}).Label(presentLabel).After(renderLabel))
}

// AddOverlay registers a system that draws on top of the entity render. Use
// it for HUDs, debug text, and other content that should appear above the
// world.
//
// Overlays run after the entity render and before the single per-frame
// present. They should write to the back buffer via Screen.SetContent (or
// via tui.Screen + tcell helpers); they should NOT call Screen.Show — that
// is the present system's job, and an extra Show in an overlay reintroduces
// the two-flush flicker the split design is meant to avoid.
func AddOverlay(a *app.App, sys app.SystemFunc) {
	a.AddSystems(schedule.PostUpdate,
		app.System(sys).After(renderLabel).Before(presentLabel))
}

// AddPreRender registers a system that runs before the entity render in
// PostUpdate. Useful for camera updates or world-state mutations that should
// be visible in this frame's output.
func AddPreRender(a *app.App, sys app.SystemFunc) {
	a.AddSystems(schedule.PostUpdate, app.System(sys).Before(renderLabel))
}

// Screen returns the tcell screen, or nil before the terminal plugin has
// initialised. Convenience for systems that only need direct screen access
// without going through the full Terminal resource.
func Screen(c *app.Ctx) tcell.Screen {
	t := app.GetResource[terminal.Terminal](c)
	if t == nil {
		return nil
	}
	return t.Screen
}

// Size returns the current terminal dimensions, or 0,0 if the terminal isn't
// ready. Convenience for systems that need bounds.
func Size(c *app.Ctx) (int, int) {
	term := app.GetResource[terminal.Terminal](c)
	if term == nil {
		return 0, 0
	}
	return term.Screen.Size()
}
