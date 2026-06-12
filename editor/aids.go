package editor

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
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
		for _, e := range [12][2]int{
			{0, 1}, {2, 3}, {4, 5}, {6, 7}, // X edges
			{0, 2}, {1, 3}, {4, 6}, {5, 7}, // Y edges
			{0, 4}, {1, 5}, {2, 6}, {3, 7}, // Z edges
		} {
			render3d.Line(c, corners[e[0]], corners[e[1]], col)
		}
	}
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
