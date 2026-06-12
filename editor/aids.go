package editor

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/collision"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// drawSelectionBox outlines each selected entity's mesh AABB (in its local
// space, transformed by the world matrix) in the scene's line pass. The
// primary selection gets the brighter box.
func drawSelectionBox(c *app.Ctx, st *state) {
	w := c.World()
	prim, _ := st.primary()
	mrMap := generic.NewMap[render3d.MeshRenderer](w)
	gtMap := generic.NewMap[render3d.GlobalTransform](w)
	for _, sel := range st.sel {
		if !w.Alive(sel) || !mrMap.Has(sel) || !gtMap.Has(sel) {
			continue
		}
		lo, hi, ok := st.meshBounds(c, mrMap.Get(sel).Mesh)
		if !ok {
			continue
		}
		model := gtMap.Get(sel).Matrix
		corners := [8]m.Vec3{}
		for i := range corners {
			p := m.Vec3{X: lo.X, Y: lo.Y, Z: lo.Z}
			if i&1 != 0 {
				p.X = hi.X
			}
			if i&2 != 0 {
				p.Y = hi.Y
			}
			if i&4 != 0 {
				p.Z = hi.Z
			}
			corners[i] = model.MulVec4(m.Vec4{X: p.X, Y: p.Y, Z: p.Z, W: 1}).XYZ()
		}
		col := m.Vec4{X: 1, Y: 0.62, Z: 0.1, W: 0.95}
		if sel != prim {
			col = m.Vec4{X: 0.95, Y: 0.55, Z: 0.1, W: 0.55}
		}
		lineBox(c, corners, col)
	}
}

// boxEdges indexes the 12 edges of an 8-corner box, where a corner's index
// bits select the high coordinate on each axis (bit 0 = X, 1 = Y, 2 = Z).
var boxEdges = [12][2]int{
	{0, 1}, {2, 3}, {4, 5}, {6, 7}, // X edges
	{0, 2}, {1, 3}, {4, 6}, {5, 7}, // Y edges
	{0, 4}, {1, 5}, {2, 6}, {3, 7}, // Z edges
}

// lineBox pushes the 12 edges of a corner-indexed box into the line pass.
func lineBox(c *app.Ctx, corners [8]m.Vec3, col m.Vec4) {
	for _, e := range boxEdges {
		render3d.Line(c, corners[e[0]], corners[e[1]], col)
	}
}

// drawColliderBoxes outlines every Collider3D's collision volume in the line
// pass: a box centered on the entity's world position spanning ±Half on each
// axis, axis-aligned and unrotated — exactly what the broad phase tests, so the
// wireframe shows the real collision bounds rather than the mesh AABB.
func drawColliderBoxes(c *app.Ctx) {
	w := c.World()
	gtMap := generic.NewMap[render3d.GlobalTransform](w)
	col := m.Vec4{X: 0.3, Y: 0.9, Z: 0.45, W: 0.8}
	app.Query1[collision.Collider3D](c, func(e ecs.Entity, cl *collision.Collider3D) {
		if !gtMap.Has(e) {
			return
		}
		mat := gtMap.Get(e).Matrix
		ctr := m.Vec3{X: mat[12], Y: mat[13], Z: mat[14]}
		h := cl.Half
		var corners [8]m.Vec3
		for i := range corners {
			p := ctr.Sub(h)
			if i&1 != 0 {
				p.X = ctr.X + h.X
			}
			if i&2 != 0 {
				p.Y = ctr.Y + h.Y
			}
			if i&4 != 0 {
				p.Z = ctr.Z + h.Z
			}
			corners[i] = p
		}
		lineBox(c, corners, col)
	})
}

// lightIconRadius is the pick radius (and icon scale) of light entities,
// which usually have no mesh for the raycaster to hit.
const lightIconRadius = float32(0.4)

// drawLightIcons marks every light's position in the scene's line pass: a
// 6-ray star for point lights, a circled direction arrow for directional
// lights. The icons double as pick targets (see pickLight).
func drawLightIcons(c *app.Ctx, st *state) {
	w := c.World()
	gtMap := generic.NewMap[render3d.GlobalTransform](w)
	star := func(p m.Vec3, r float32, col m.Vec4) {
		for _, d := range [3]m.Vec3{{X: r}, {Y: r}, {Z: r}} {
			render3d.Line(c, p.Sub(d), p.Add(d), col)
		}
		k := r * 0.55
		for _, d := range [4]m.Vec3{{X: k, Y: k, Z: k}, {X: -k, Y: k, Z: k}, {X: k, Y: k, Z: -k}, {X: -k, Y: k, Z: -k}} {
			render3d.Line(c, p.Sub(d), p.Add(d), col)
		}
	}
	app.Query1[render3d.PointLight](c, func(e ecs.Entity, pl *render3d.PointLight) {
		if !gtMap.Has(e) {
			return
		}
		mat := gtMap.Get(e).Matrix
		p := m.Vec3{X: mat[12], Y: mat[13], Z: mat[14]}
		star(p, lightIconRadius, lightColor(pl.Color))
	})
	app.Query1[render3d.DirectionalLight](c, func(e ecs.Entity, dl *render3d.DirectionalLight) {
		if !gtMap.Has(e) {
			return
		}
		mat := gtMap.Get(e).Matrix
		p := m.Vec3{X: mat[12], Y: mat[13], Z: mat[14]}
		col := lightColor(dl.Color)
		star(p, lightIconRadius*0.7, col)
		// The cast direction is the entity's forward (-Z) axis, so the arrow
		// follows the gizmo's rotation.
		dir := render3d.Forward(mat)
		if dir != (m.Vec3{}) {
			render3d.Line(c, p, p.Add(dir.Scale(lightIconRadius*4)), col)
		}
	})
}

// lightColor renders a light's color as a bright, alpha-solid line color.
func lightColor(col m.Vec3) m.Vec4 {
	c := col
	if c == (m.Vec3{}) {
		c = m.Vec3{X: 1, Y: 1, Z: 1}
	}
	n := max(c.X, max(c.Y, c.Z))
	if n > 0 {
		c = c.Scale(1 / n)
	}
	return m.Vec4{X: c.X, Y: c.Y, Z: c.Z, W: 0.95}
}

// pickLight tests the pick ray against every light's icon sphere, so lights
// are selectable even without a mesh. Returns the nearest hit distance.
func pickLight(c *app.Ctx, origin, dir m.Vec3) (ecs.Entity, float32, bool) {
	w := c.World()
	gtMap := generic.NewMap[render3d.GlobalTransform](w)
	d := dir.Normalize()
	var best ecs.Entity
	bestT := float32(0)
	found := false
	test := func(e ecs.Entity) {
		if !gtMap.Has(e) {
			return
		}
		mat := gtMap.Get(e).Matrix
		p := m.Vec3{X: mat[12], Y: mat[13], Z: mat[14]}
		// Ray-sphere: closest approach of the ray to the icon center.
		oc := p.Sub(origin)
		t := oc.Dot(d)
		if t <= 0 {
			return
		}
		if oc.Sub(d.Scale(t)).Length() > lightIconRadius {
			return
		}
		if !found || t < bestT {
			best, bestT, found = e, t, true
		}
	}
	app.Query1[render3d.PointLight](c, func(e ecs.Entity, _ *render3d.PointLight) { test(e) })
	app.Query1[render3d.DirectionalLight](c, func(e ecs.Entity, _ *render3d.DirectionalLight) { test(e) })
	return best, bestT, found
}

// meshBounds returns the cached model-space AABB of a registered mesh.
func (st *state) meshBounds(c *app.Ctx, ref string) (lo, hi m.Vec3, ok bool) {
	if b, hit := st.aabbMeshCache[ref]; hit {
		return b[0], b[1], true
	}
	meshes := app.GetResource[render3d.MeshRegistry](c)
	if meshes == nil {
		return lo, hi, false
	}
	mesh := meshes.CPU(ref)
	if mesh == nil || len(mesh.Vertices) == 0 {
		return lo, hi, false
	}
	v0 := mesh.Vertices[0]
	lo = m.Vec3{X: v0.PX, Y: v0.PY, Z: v0.PZ}
	hi = lo
	for _, v := range mesh.Vertices[1:] {
		lo.X, lo.Y, lo.Z = min(lo.X, v.PX), min(lo.Y, v.PY), min(lo.Z, v.PZ)
		hi.X, hi.Y, hi.Z = max(hi.X, v.PX), max(hi.Y, v.PY), max(hi.Z, v.PZ)
	}
	st.aabbMeshCache[ref] = [2]m.Vec3{lo, hi}
	return lo, hi, true
}

// drawGrid pushes the ground-plane reference grid into the scene's line pass
// each frame: 1-unit cells over a 20x20 area with emphasized world axes.
func drawGrid(c *app.Ctx) {
	const half = float32(10)
	minor := m.Vec4{X: 0.32, Y: 0.34, Z: 0.38, W: 0.6}
	xAxis := m.Vec4{X: 0.85, Y: 0.3, Z: 0.3, W: 0.9}
	zAxis := m.Vec4{X: 0.35, Y: 0.5, Z: 0.9, W: 0.9}
	for i := -int(half); i <= int(half); i++ {
		f := float32(i)
		cz := minor
		if i == 0 {
			cz = zAxis
		}
		render3d.Line(c, m.Vec3{X: f, Z: -half}, m.Vec3{X: f, Z: half}, cz)
		cx := minor
		if i == 0 {
			cx = xAxis
		}
		render3d.Line(c, m.Vec3{X: -half, Z: f}, m.Vec3{X: half, Z: f}, cx)
	}
}
