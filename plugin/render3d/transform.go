package render3d

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// maxHierarchyDepth bounds the parent walk so a malformed (cyclic) parent chain
// can't loop forever; 64 is far deeper than any real scene graph.
const maxHierarchyDepth = 64

// propagateTransforms computes each entity's world matrix into GlobalTransform
// from its local Transform3D and any Parent chain. It runs in PostUpdate before
// the render pass.
//
// Flat scenes (no Parent components) take a fast path: the local matrix is the
// world matrix, with no extra allocation. When parents exist, parented entities
// are resolved by walking up the chain and memoizing, so a child always sees its
// parent's already-composed world matrix regardless of iteration order.
//
// Limitation (foundation): the resolver tolerates arbitrary order via recursion
// with a depth cap, not a topologically-sorted hierarchy resource. Deep, heavily
// reparented graphs would benefit from a dedicated hierarchy structure later.
func propagateTransforms(c *app.Ctx) {
	world := c.World()
	parents := generic.NewMap[Parent](world)
	gt := generic.NewMap[GlobalTransform](world)

	var parented []ecs.Entity
	app.Query2[Transform3D, GlobalTransform](c, func(e ecs.Entity, t *Transform3D, g *GlobalTransform) {
		// Seed every GlobalTransform with the LOCAL matrix. Roots are done; for
		// parented entities this is fixed up below into the world matrix.
		g.Matrix = t.matrix()
		if parents.Has(e) {
			parented = append(parented, e)
		}
	})
	if len(parented) == 0 {
		return
	}

	resolved := make(map[ecs.Entity]bool, len(parented))
	var resolve func(e ecs.Entity, depth int) m.Mat4
	resolve = func(e ecs.Entity, depth int) m.Mat4 {
		g := gt.Get(e)
		if g == nil {
			return m.Identity4()
		}
		// A root, an already-resolved entity, or one past the depth cap holds its
		// world matrix already.
		if resolved[e] || depth >= maxHierarchyDepth || !parents.Has(e) {
			return g.Matrix
		}
		local := g.Matrix
		p := parents.Get(e).Entity
		if p != e && world.Alive(p) && gt.Has(p) {
			g.Matrix = resolve(p, depth+1).Mul(local)
		}
		resolved[e] = true
		return g.Matrix
	}
	for _, e := range parented {
		resolve(e, 0)
	}
}
