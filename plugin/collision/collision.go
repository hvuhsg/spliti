// Package collision is a reusable, GPU-free broad-phase collision plugin.
//
// It detects overlapping axis-aligned bounding boxes and emits an event per
// overlapping pair each FixedUpdate tick. A uniform spatial-hash grid keeps the
// broad phase near O(n) instead of the naive O(n²) pair-test, so scenes with
// hundreds of bodies stay cheap. Bodies can be filtered with collision
// layers/masks so unrelated entities never generate pairs.
//
// Two coordinate spaces are supported and live side by side:
//
//	2D  Collider     over tui.Position (integer grid) → CollisionEvent
//	3D  Collider3D    over a caller-supplied position  → Collision3DEvent
//
// The package imports only the pure-Go math library plugin/render3d/m (never
// the GPU renderer), so it builds and tests without cgo and is safe to drop
// into any backend.
//
// Out of scope (see docs/roadmap.md): physics resolution (velocity, gravity,
// restitution), non-box shapes (circles, rotated bodies), and continuous
// collision for fast movers.
package collision

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
)

// DefaultCellSize is the broad-phase grid cell size used when Config.CellSize is
// left zero. It is a coarse default suited to terminal-scale worlds; tune it to
// roughly the typical body size for best performance.
const DefaultCellSize = 16

// Collider is an axis-aligned bounding box whose top-left corner sits at the
// entity's tui.Position. W and H are its size in cells.
//
// Layer and Mask filter which entities collide. Layer is the bitset of layers
// this body belongs to; Mask is the bitset of layers it collides with. Two
// bodies generate a pair only when each one's mask includes a layer of the
// other. The zero value (Layer 0, Mask 0) means "default layer, collide with
// everything", so a plain Collider{W,H} behaves like an unfiltered box.
type Collider struct {
	W, H int
	// The `spliti` tags mark these for the editor: a "layer" field holds one
	// named layer bit, a "layers" field a mask of them, and scene source
	// written by the editor uses the game's named layer constants.
	Layer uint32 `spliti:"layer"`
	Mask  uint32 `spliti:"layers"`
}

// CollisionEvent is sent once per overlapping pair per tick. A and B are the
// participating entities in ascending iteration order (so a pair is reported as
// (A,B), never also as (B,A)). ATag and BTag are optional human-readable labels
// resolved through Config.Tag (empty when no resolver is set).
type CollisionEvent struct {
	A, B       ecs.Entity
	ATag, BTag string
}

// Config tunes the 2D collision system.
type Config struct {
	// CellSize is the broad-phase grid cell size. Zero selects DefaultCellSize.
	CellSize int
	// Tag, when non-nil, resolves an entity's label for CollisionEvent.ATag/BTag.
	// Leave nil to emit empty tags.
	Tag func(*ecs.World, ecs.Entity) string
}

// NewSystem returns the 2D collision system: it collects every
// (tui.Position, Collider) entity, runs the spatial-hash broad phase, and emits
// a CollisionEvent for each overlapping, layer-compatible pair. Returning a
// SystemFunc lets callers control labelling and ordering instead of taking
// the bundled Plugin.
func NewSystem(cfg Config) app.SystemFunc {
	cell := cfg.CellSize
	if cell <= 0 {
		cell = DefaultCellSize
	}
	tag := cfg.Tag
	return func(c *app.Ctx) {
		type meta struct {
			e   ecs.Entity
			tag string
		}
		var bodies []aabb2
		var infos []meta
		world := c.World()
		app.Query2[tui.Position, Collider](c, func(e ecs.Entity, p *tui.Position, col *Collider) {
			bodies = append(bodies, aabb2{
				minX: p.X, minY: p.Y,
				maxX: p.X + col.W, maxY: p.Y + col.H,
				layer: col.Layer, mask: col.Mask,
			})
			t := ""
			if tag != nil {
				t = tag(world, e)
			}
			infos = append(infos, meta{e: e, tag: t})
		})
		pairs2(bodies, cell, func(i, j int) {
			app.SendEvent(c, CollisionEvent{
				A: infos[i].e, B: infos[j].e,
				ATag: infos[i].tag, BTag: infos[j].tag,
			})
		})
	}
}

// Plugin installs the 2D collision system on FixedUpdate. RunIf, when non-nil,
// gates the system (e.g. an editor pausing the simulation). Configure the broad
// phase and tag resolver through the embedded Config.
type Plugin struct {
	RunIf func(*app.Ctx) bool
	Config
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	sys := app.System(NewSystem(p.Config)).Label("__spliti_collision")
	if p.RunIf != nil {
		sys.RunIf(p.RunIf)
	}
	a.AddSystems(schedule.FixedUpdate, sys)
}
