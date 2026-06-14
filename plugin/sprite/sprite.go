// Package sprite adds multi-cell ASCII-art rendering to the spliti engine.
//
// Today the tui plugin draws one rune per entity. Sprites let an entity carry
// a 2D grid of (rune, style) cells and render the whole grid at the entity's
// Position. The grid lives in a SpriteAsset registered in a SpriteRegistry
// resource keyed by string Ref; entities reference the asset by Ref so many
// entities can share one asset.
//
// Add sprite.Plugin{} to register the SpriteRegistry resource. The actual
// drawing is performed by the tui plugin (which reads the registry as part
// of its existing render pass), so sprite z-order and viewport-clipping
// behave identically to plain Glyph entities.
//
// Animation is additive: attach a SpriteAnimation alongside a Sprite and the
// plugin's Update system cycles Sprite.Ref through the active Clip's frames
// (see animation.go). Because it rewrites Sprite.Ref, every backend that reads
// Sprite.Ref animates with no renderer change. Frame selection advances on
// wall-clock Time.Delta(), not Time.Alpha(): frames are discrete pictures, so
// there is nothing to interpolate between them — Alpha() smooths *continuous*
// fixed-step quantities (positions) onto the render rate, which does not apply
// to picking which whole asset to show.
package sprite

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Cell is one position in a sprite's grid. Empty=true means the cell is
// transparent — the renderer skips it so background underneath shows through.
type Cell struct {
	Char  rune
	Style tcell.Style
	Empty bool
}

// SpriteAsset is the loaded grid for one Ref. Cells is row-major, len == W*H.
type SpriteAsset struct {
	W, H  int
	Cells []Cell
}

// At returns the cell at (x,y); panics on out-of-bounds.
func (s *SpriteAsset) At(x, y int) Cell { return s.Cells[y*s.W+x] }

// Sprite is a component placing a sprite asset at the entity's tui.Position.
// AnchorX/AnchorY shift the asset's local (0,0) so the same Ref can be drawn
// with different attach points (top-left, center, etc.) from one asset.
type Sprite struct {
	Ref              string
	AnchorX, AnchorY int
}

// SpriteRegistry resolves Ref strings to *SpriteAsset. The tui renderer reads
// it once per frame; sprite editors and project loaders mutate it through
// Set/Get/Delete.
type SpriteRegistry struct {
	byRef map[string]*SpriteAsset
}

// NewRegistry constructs an empty registry.
func NewRegistry() *SpriteRegistry {
	return &SpriteRegistry{byRef: make(map[string]*SpriteAsset)}
}

// Set inserts or replaces an asset under ref.
func (r *SpriteRegistry) Set(ref string, asset *SpriteAsset) {
	r.byRef[ref] = asset
}

// Get returns the asset for ref, or nil if not registered.
func (r *SpriteRegistry) Get(ref string) *SpriteAsset {
	if r == nil {
		return nil
	}
	return r.byRef[ref]
}

// Delete removes ref from the registry.
func (r *SpriteRegistry) Delete(ref string) { delete(r.byRef, ref) }

// Refs returns all registered refs in unspecified order.
func (r *SpriteRegistry) Refs() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.byRef))
	for k := range r.byRef {
		out = append(out, k)
	}
	return out
}

// Plugin installs an empty SpriteRegistry resource so the tui renderer can
// read it without nil-checks. Add this BEFORE the project loader populates
// it, and BEFORE tui.Plugin runs (the renderer tolerates an empty registry,
// but installing the resource up front avoids "first frame missing" gotchas).
type Plugin struct{}

// Build implements app.Plugin.
func (Plugin) Build(a *app.App) {
	if !app.HasResource[SpriteRegistry](a) {
		app.InsertResource(a, NewRegistry())
	}
	a.AddSystems(schedule.Update, app.System(animateSprites).Label("__spliti_sprite_animation"))
}

// animateSprites advances every SpriteAnimation by the frame delta and writes the
// resulting frame into the entity's Sprite.Ref. Runs in Update — after Time's
// First-stage tick refreshes Delta, and before the tui renderer's PostUpdate pass
// reads Sprite.Ref. Entities lacking a Sprite (or with no Time resource) are
// skipped.
func animateSprites(c *app.Ctx) {
	t := app.GetResource[time.Time](c)
	if t == nil {
		return
	}
	dt := t.Delta()
	sprites := generic.NewMap[Sprite](c.World())
	app.Query1[SpriteAnimation](c, func(e ecs.Entity, anim *SpriteAnimation) {
		ref, ok := anim.Advance(dt)
		if !ok {
			return
		}
		if sprites.Has(e) {
			sprites.Get(e).Ref = ref
		}
	})
}
