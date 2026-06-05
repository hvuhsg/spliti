package render3d

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
)

// Hit is the nearest intersection found by Raycast.
type Hit struct {
	Entity ecs.Entity
	Point  m.Vec3  // world-space intersection point
	Normal m.Vec3  // world-space surface normal (unit)
	Dist   float32 // distance from the ray origin, along the normalized direction
}

// ScreenToRay unprojects a window pixel (px,py, top-left origin) into a world-
// space ray using the active camera and the window size. Pass the X,Y of a
// MouseButtonEvent/MouseMoveEvent (which are in window coordinates). Returns a
// degenerate ray if the renderer is not ready.
func ScreenToRay(c *app.Ctx, px, py float64) (origin, dir m.Vec3) {
	cam := app.GetResource[Camera3D](c)
	g := app.GetResource[GPU](c)
	if cam == nil || g == nil || g.window == nil {
		return m.Vec3{}, m.Vec3{Z: -1}
	}
	w, h := g.window.GetSize()
	return cam.ScreenToRay(px, py, w, h)
}

// Raycast casts a world-space ray against every entity that has a MeshRenderer +
// GlobalTransform with retained CPU geometry, returning the nearest hit. dir need
// not be normalized. It is brute force over all entities and triangles — fine for
// a street of boxes; a BVH over entity AABBs is the natural optimization later.
func Raycast(c *app.Ctx, origin, dir m.Vec3) (Hit, bool) {
	meshes := app.GetResource[MeshRegistry](c)
	if meshes == nil {
		return Hit{}, false
	}
	d := dir.Normalize()
	if d == (m.Vec3{}) {
		return Hit{}, false
	}

	var best Hit
	found := false
	bestT := float32(math.MaxFloat32)

	app.Query2[MeshRenderer, GlobalTransform](c, func(e ecs.Entity, mr *MeshRenderer, gt *GlobalTransform) {
		mesh := meshes.CPU(mr.Mesh)
		if mesh == nil {
			return
		}
		inv, ok := gt.Matrix.Inverse()
		if !ok {
			return
		}
		// Transform the ray into the entity's local space; intersect there, then
		// map the hit back to world space so non-uniform scale is handled.
		lo := mulPoint(inv, origin)
		ld := mulDir(inv, d)
		t, n, hit := raycastMesh(mesh, lo, ld)
		if !hit {
			return
		}
		worldHit := mulPoint(gt.Matrix, lo.Add(ld.Scale(t)))
		worldT := worldHit.Sub(origin).Dot(d) // signed distance along the ray
		if worldT <= 1e-4 || worldT >= bestT {
			return
		}
		bestT = worldT
		nm := m.NormalMatrix(gt.Matrix)
		best = Hit{
			Entity: e,
			Point:  worldHit,
			Normal: nm.MulVec3(n).Normalize(),
			Dist:   worldT,
		}
		found = true
	})
	return best, found
}

// raycastMesh returns the nearest forward intersection of the local-space ray
// with the mesh triangles: the ray parameter t, the geometric triangle normal,
// and whether any triangle was hit. Pure (no GPU), so it is unit-testable.
func raycastMesh(mesh *Mesh, origin, dir m.Vec3) (tHit float32, normal m.Vec3, ok bool) {
	bestT := float32(math.MaxFloat32)
	for i := 0; i+2 < len(mesh.Indices); i += 3 {
		a := vertexPos(mesh.Vertices[mesh.Indices[i]])
		b := vertexPos(mesh.Vertices[mesh.Indices[i+1]])
		cc := vertexPos(mesh.Vertices[mesh.Indices[i+2]])
		t, hit := rayTriangle(origin, dir, a, b, cc)
		if hit && t > 1e-5 && t < bestT {
			bestT = t
			normal = b.Sub(a).Cross(cc.Sub(a)).Normalize()
			ok = true
		}
	}
	return bestT, normal, ok
}

// rayTriangle is the Möller–Trumbore ray/triangle test, double-sided (a back-face
// is still a hit). Returns the ray parameter t (>=0 for a forward hit) and
// whether the ray crosses the triangle.
func rayTriangle(orig, dir, v0, v1, v2 m.Vec3) (float32, bool) {
	const eps = 1e-8
	e1 := v1.Sub(v0)
	e2 := v2.Sub(v0)
	p := dir.Cross(e2)
	det := e1.Dot(p)
	if det > -eps && det < eps {
		return 0, false // ray parallel to the triangle plane
	}
	invDet := 1 / det
	tv := orig.Sub(v0)
	u := tv.Dot(p) * invDet
	if u < 0 || u > 1 {
		return 0, false
	}
	q := tv.Cross(e1)
	v := dir.Dot(q) * invDet
	if v < 0 || u+v > 1 {
		return 0, false
	}
	t := e2.Dot(q) * invDet
	if t < 0 {
		return 0, false
	}
	return t, true
}

func vertexPos(v Vertex) m.Vec3 { return m.Vec3{X: v.PX, Y: v.PY, Z: v.PZ} }

// mulPoint transforms a point (w=1) by an affine matrix.
func mulPoint(mat m.Mat4, p m.Vec3) m.Vec3 {
	return mat.MulVec4(m.Vec4{X: p.X, Y: p.Y, Z: p.Z, W: 1}).XYZ()
}

// mulDir transforms a direction (w=0, ignores translation) by a matrix.
func mulDir(mat m.Mat4, d m.Vec3) m.Vec3 {
	return mat.MulVec4(m.Vec4{X: d.X, Y: d.Y, Z: d.Z, W: 0}).XYZ()
}
