package collision

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
)

// DefaultCellSize3D is the broad-phase cell size used when Config3D.CellSize is
// left zero, in world units.
const DefaultCellSize3D float32 = 4

// Collider3D is an axis-aligned bounding box centered on the entity's world
// position, extending Half in each axis (so the full box spans 2·Half). Layer
// and Mask filter collisions exactly as on the 2D Collider.
type Collider3D struct {
	Half m.Vec3
	// Tagged for the editor like the 2D Collider: scene source uses the
	// game's named layer constants for these bit fields.
	Layer uint32 `spliti:"layer"`
	Mask  uint32 `spliti:"layers"`
}

// Collision3DEvent is sent once per overlapping pair per tick, with A and B in
// ascending iteration order.
type Collision3DEvent struct {
	A, B ecs.Entity
}

// Config3D tunes the 3D collision system.
type Config3D struct {
	// CellSize is the broad-phase cell size in world units. Zero selects
	// DefaultCellSize3D.
	CellSize float32
	// Pos resolves an entity's world-space center. It is REQUIRED: the
	// collision package never imports the (cgo) renderer, so the caller wires
	// this to read its own transform — e.g. render3d.Transform3D.Translation.
	Pos func(*ecs.World, ecs.Entity) m.Vec3
}

// NewSystem3D returns the 3D collision system. It collects every Collider3D
// entity, reads each center through Config3D.Pos, runs the spatial-hash broad
// phase, and emits a Collision3DEvent per overlapping, layer-compatible pair.
//
// It panics if Pos is nil — without a position source there is nothing to test.
func NewSystem3D(cfg Config3D) app.SystemFunc {
	cell := cfg.CellSize
	if cell <= 0 {
		cell = DefaultCellSize3D
	}
	pos := cfg.Pos
	if pos == nil {
		panic("collision.NewSystem3D: Config3D.Pos must be set")
	}
	return func(c *app.Ctx) {
		var bodies []aabb3
		var ents []ecs.Entity
		world := c.World()
		app.Query1[Collider3D](c, func(e ecs.Entity, col *Collider3D) {
			ctr := pos(world, e)
			bodies = append(bodies, aabb3{
				min:   ctr.Sub(col.Half),
				max:   ctr.Add(col.Half),
				layer: col.Layer, mask: col.Mask,
			})
			ents = append(ents, e)
		})
		pairs3(bodies, cell, func(i, j int) {
			app.SendEvent(c, Collision3DEvent{A: ents[i], B: ents[j]})
		})
	}
}

// Plugin3D installs the 3D collision system on FixedUpdate. RunIf, when
// non-nil, gates the system. Config3D.Pos must be set.
type Plugin3D struct {
	RunIf func(*app.Ctx) bool
	Config3D
}

// Build implements app.Plugin.
func (p Plugin3D) Build(a *app.App) {
	sys := app.System(NewSystem3D(p.Config3D)).Label("__spliti_collision3d")
	if p.RunIf != nil {
		sys.RunIf(p.RunIf)
	}
	a.AddSystems(schedule.FixedUpdate, sys)
}
