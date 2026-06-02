// Package tui renders entities with Position + Glyph or Position + Sprite to
// the terminal screen owned by the terminal plugin. Add tui.Plugin{} after
// terminal.Plugin{}.
//
// If a *viewport.Viewport resource is present and Active, the renderer clears
// only that rect, clips entity coordinates to it, and offsets every
// SetContent call by the rect's origin. Otherwise the renderer fills the
// full screen as before.
//
// If a *sprite.SpriteRegistry resource is present, entities with a Sprite
// component are expanded into the same draw passes as Glyph entities so
// z-ordering and clipping are unified.
package tui

import (
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/sprite"
	"github.com/hvuhsg/spliti/plugin/terminal"
	"github.com/hvuhsg/spliti/viewport"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Position places an entity in the terminal grid. (0,0) is the top-left cell
// of the active rendering region (full screen, or the viewport rect).
type Position struct{ X, Y int }

// Glyph is the visual representation of a single-cell entity. Style controls
// fg/bg color and attributes; pass tcell.StyleDefault if you don't care.
type Glyph struct {
	Char  rune
	Style tcell.Style
}

// Layer overrides the draw order of a (Position, Glyph) or (Position, Sprite)
// entity. Higher Z draws on top of lower Z and on top of unlayered entities.
//
// Entities without a Layer render first via the unsorted fast path. Entities
// with a Layer are collected, stable-sorted by Z ascending, then drawn on top.
// If no entity in the world has a Layer component, the renderer skips the
// layered pass entirely — there is no per-frame allocation for the common
// case.
type Layer struct{ Z int }

// renderLabel marks the entity-render system. Kept package-private so users
// extend the renderer through AddOverlay / AddPreRender instead of writing
// raw After/Before strings.
const (
	renderLabel  = "__spliti_render"
	presentLabel = "__spliti_present"
)

// layeredCell is a snapshot of a layered cell (from a Glyph entity or one
// cell of a Sprite asset) for the per-frame sort. The render system reuses
// one buffer across frames.
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

// clipRect captures the active draw region. Resolved once per render call
// from the optional Viewport resource so neither the fast path nor the
// layered pass needs to recheck whether a viewport is set.
type clipRect struct {
	offX, offY int // screen-coords origin of cell (0,0)
	w, h       int // size of the region in world coords
}

// fits reports whether (x,y) in world coords lies inside the clip and
// returns the screen-coords destination. Inlined by the compiler at every
// call site.
func (r clipRect) fits(x, y int) (sx, sy int, ok bool) {
	if x < 0 || y < 0 || x >= r.w || y >= r.h {
		return 0, 0, false
	}
	return x + r.offX, y + r.offY, true
}

// resolveClip reads the viewport resource if present and falls back to the
// full screen size. Active=false viewports also fall back, so callers can
// leave a Viewport resource registered but inert.
func resolveClip(c *app.Ctx, s tcell.Screen) clipRect {
	w, h := s.Size()
	vp := app.GetResource[viewport.Viewport](c)
	if vp == nil || !vp.Active {
		return clipRect{0, 0, w, h}
	}
	// Defensive clamp: a viewport that extends past the screen is reduced
	// rather than panicking on SetContent.
	cw, ch := vp.W, vp.H
	if vp.X+cw > w {
		cw = w - vp.X
	}
	if vp.Y+ch > h {
		ch = h - vp.Y
	}
	if cw < 0 {
		cw = 0
	}
	if ch < 0 {
		ch = 0
	}
	return clipRect{offX: vp.X, offY: vp.Y, w: cw, h: ch}
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	clearStyle := p.ClearStyle

	// Reused across frames so the layered pass amortises its slice
	// allocation.
	layered := make([]layeredCell, 0, 32)
	unlayeredGlyphFilter := generic.NewFilter2[Position, Glyph]().Without(generic.T[Layer]())
	unlayeredSpriteFilter := generic.NewFilter2[Position, sprite.Sprite]().Without(generic.T[Layer]())

	a.AddSystems(schedule.PostUpdate, app.System(func(c *app.Ctx) {
		term := app.GetResource[terminal.Terminal](c)
		if term == nil {
			return
		}
		s := term.Screen
		clip := resolveClip(c, s)

		// Clear the active region. We always clear by writing spaces — never
		// Screen.Clear() — because Clear wipes the entire back buffer and
		// would erase chrome the editor draws outside the viewport.
		for y := 0; y < clip.h; y++ {
			for x := 0; x < clip.w; x++ {
				s.SetContent(x+clip.offX, y+clip.offY, ' ', nil, clearStyle)
			}
		}

		// Optional sprite registry. nil means no sprite entities will draw
		// (registry not installed) — Sprite components are still allowed in
		// the world, they just don't render.
		registry := app.GetResource[sprite.SpriteRegistry](c)

		// --- Fast path: unlayered glyphs. No allocation, no sort. -----
		uq := unlayeredGlyphFilter.Query(c.World())
		for uq.Next() {
			pos, g := uq.Get()
			if sx, sy, ok := clip.fits(pos.X, pos.Y); ok {
				s.SetContent(sx, sy, g.Char, nil, g.Style)
			}
		}

		// --- Fast path: unlayered sprites. ---------------------------
		if registry != nil {
			usq := unlayeredSpriteFilter.Query(c.World())
			for usq.Next() {
				pos, sp := usq.Get()
				asset := registry.Get(sp.Ref)
				if asset == nil {
					continue
				}
				drawSpriteFast(s, clip, pos.X-sp.AnchorX, pos.Y-sp.AnchorY, asset)
			}
		}

		// --- Layered pass: collect, stable-sort by Z, draw. -----------
		layered = layered[:0]
		app.Query3[Position, Glyph, Layer](c, func(_ ecs.Entity, pos *Position, g *Glyph, l *Layer) {
			if _, _, ok := clip.fits(pos.X, pos.Y); !ok {
				return
			}
			layered = append(layered, layeredCell{x: pos.X, y: pos.Y, ch: g.Char, style: g.Style, z: l.Z})
		})
		if registry != nil {
			app.Query3[Position, sprite.Sprite, Layer](c, func(_ ecs.Entity, pos *Position, sp *sprite.Sprite, l *Layer) {
				asset := registry.Get(sp.Ref)
				if asset == nil {
					return
				}
				layered = collectSpriteCells(layered, clip, pos.X-sp.AnchorX, pos.Y-sp.AnchorY, asset, l.Z)
			})
		}
		if len(layered) > 0 {
			sort.SliceStable(layered, func(i, j int) bool { return layered[i].z < layered[j].z })
			for i := range layered {
				lc := &layered[i]
				// Already clipped at collection time, but offset is applied here.
				s.SetContent(lc.x+clip.offX, lc.y+clip.offY, lc.ch, nil, lc.style)
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

// drawSpriteFast writes asset cells directly to the screen for the unlayered
// pass. originX/originY is the world-coords position of the asset's (0,0)
// cell after applying anchor.
func drawSpriteFast(s tcell.Screen, clip clipRect, originX, originY int, asset *sprite.SpriteAsset) {
	for cy := 0; cy < asset.H; cy++ {
		for cx := 0; cx < asset.W; cx++ {
			cell := asset.Cells[cy*asset.W+cx]
			if cell.Empty {
				continue
			}
			if sx, sy, ok := clip.fits(originX+cx, originY+cy); ok {
				s.SetContent(sx, sy, cell.Char, nil, cell.Style)
			}
		}
	}
}

// collectSpriteCells appends each non-empty asset cell to the layered buffer
// at the entity's Z. Returns the (possibly grown) slice.
func collectSpriteCells(buf []layeredCell, clip clipRect, originX, originY int, asset *sprite.SpriteAsset, z int) []layeredCell {
	for cy := 0; cy < asset.H; cy++ {
		for cx := 0; cx < asset.W; cx++ {
			cell := asset.Cells[cy*asset.W+cx]
			if cell.Empty {
				continue
			}
			wx, wy := originX+cx, originY+cy
			if _, _, ok := clip.fits(wx, wy); !ok {
				continue
			}
			buf = append(buf, layeredCell{x: wx, y: wy, ch: cell.Char, style: cell.Style, z: z})
		}
	}
	return buf
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
