// Package sim is a GPU-free, unit-tested radio-propagation model: a scene made of
// planar faces with assigned materials, line-of-sight queries, and pluggable
// propagation engines (an exact image-method engine and, later, a real-time
// shooting-bouncing-rays engine) that share one per-interaction physics layer
// (Fresnel reflection/transmission, UTD diffraction). It never imports the
// renderer, so it runs (and tests) without a GPU.
//
// Units are SI throughout: meters, hertz, watts, seconds. Vectors reuse the
// render3d linear-algebra package m so scene geometry and renderer geometry share
// one coordinate convention (right-handed, +Y up).
//
// The geometry and BVH here are ported from the simpler radio3d/prop model; the
// material, Fresnel, diffraction, receiver, and SBR layers are what make this a
// physically-accurate, broadband, real-time simulator rather than a teaching toy.
package sim

import (
	"math"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// SpeedOfLight is c in m/s, the propagation speed used for delay and wavelength.
const SpeedOfLight = 299792458.0

// Face is a planar rectangle: a corner P0 with two edge vectors U and V spanning
// it (a point on the face is P0 + s·U + t·V for s,t in [0,1]). Normal is the unit
// normal U×V. Buildings are boxes of 6 faces; the ground is one large face.
type Face struct {
	P0, U, V m.Vec3
	Normal   m.Vec3
}

// NewFace builds a Face from a corner and two edge vectors, computing the unit
// normal as U×V (so the winding determines which way Normal points).
func NewFace(p0, u, v m.Vec3) Face {
	return Face{P0: p0, U: u, V: v, Normal: u.Cross(v).Normalize()}
}

// Corners returns the four rectangle corners in winding order.
func (f Face) Corners() [4]m.Vec3 {
	return [4]m.Vec3{
		f.P0,
		f.P0.Add(f.U),
		f.P0.Add(f.U).Add(f.V),
		f.P0.Add(f.V),
	}
}

// AABB returns the axis-aligned bounding box of the face.
func (f Face) AABB() AABB {
	c := f.Corners()
	bb := AABB{Min: c[0], Max: c[0]}
	for _, p := range c[1:] {
		bb = bb.Expand(p)
	}
	return bb
}

// Box returns the six outward-facing rectangle faces of an axis-aligned box from
// min to max — a building. Each face's normal points away from the box interior.
func Box(min, max m.Vec3) []Face {
	dx := max.X - min.X
	dy := max.Y - min.Y
	dz := max.Z - min.Z
	return []Face{
		// -X (normal -X): corner at (min.x,min.y,min.z), U along +Z, V along +Y.
		NewFace(m.Vec3{X: min.X, Y: min.Y, Z: min.Z}, m.Vec3{Z: dz}, m.Vec3{Y: dy}),
		// +X (normal +X): corner at (max.x,min.y,max.z), U along -Z, V along +Y.
		NewFace(m.Vec3{X: max.X, Y: min.Y, Z: max.Z}, m.Vec3{Z: -dz}, m.Vec3{Y: dy}),
		// -Z (normal -Z): corner at (max.x,min.y,min.z), U along -X, V along +Y.
		NewFace(m.Vec3{X: max.X, Y: min.Y, Z: min.Z}, m.Vec3{X: -dx}, m.Vec3{Y: dy}),
		// +Z (normal +Z): corner at (min.x,min.y,max.z), U along +X, V along +Y.
		NewFace(m.Vec3{X: min.X, Y: min.Y, Z: max.Z}, m.Vec3{X: dx}, m.Vec3{Y: dy}),
		// +Y top (normal +Y): corner at (min.x,max.y,max.z), U along +X, V along -Z.
		NewFace(m.Vec3{X: min.X, Y: max.Y, Z: max.Z}, m.Vec3{X: dx}, m.Vec3{Z: -dz}),
		// -Y bottom (normal -Y): corner at (min.x,min.y,min.z), U along +X, V along +Z.
		NewFace(m.Vec3{X: min.X, Y: min.Y, Z: min.Z}, m.Vec3{X: dx}, m.Vec3{Z: dz}),
	}
}

// AABB is an axis-aligned bounding box.
type AABB struct{ Min, Max m.Vec3 }

// Expand returns the box grown to contain p.
func (b AABB) Expand(p m.Vec3) AABB {
	return AABB{
		Min: m.Vec3{X: minf(b.Min.X, p.X), Y: minf(b.Min.Y, p.Y), Z: minf(b.Min.Z, p.Z)},
		Max: m.Vec3{X: maxf(b.Max.X, p.X), Y: maxf(b.Max.Y, p.Y), Z: maxf(b.Max.Z, p.Z)},
	}
}

// Union returns the box containing both b and o.
func (b AABB) Union(o AABB) AABB {
	return b.Expand(o.Min).Expand(o.Max)
}

// Center returns the box midpoint.
func (b AABB) Center() m.Vec3 { return b.Min.Add(b.Max).Scale(0.5) }

// mirror reflects a point across the infinite plane of f (the image method's
// source image). The result is on the opposite side of the plane at equal
// distance.
func mirror(p m.Vec3, f Face) m.Vec3 {
	d := p.Sub(f.P0).Dot(f.Normal)
	return p.Sub(f.Normal.Scale(2 * d))
}

// rayFace intersects an infinite ray (origin + t·dir, t>=0) with the face
// rectangle. It returns the ray parameter t, the hit point, and whether the ray
// crosses the rectangle. dir need not be normalized.
func rayFace(origin, dir m.Vec3, f Face) (t float32, point m.Vec3, ok bool) {
	denom := dir.Dot(f.Normal)
	if denom > -1e-9 && denom < 1e-9 {
		return 0, m.Vec3{}, false // parallel to the plane
	}
	t = f.P0.Sub(origin).Dot(f.Normal) / denom
	if t < 0 {
		return 0, m.Vec3{}, false
	}
	pt := origin.Add(dir.Scale(t))
	if !pointInRect(pt, f) {
		return 0, m.Vec3{}, false
	}
	return t, pt, true
}

// segmentFacePoint returns the intersection of the open segment a→b with the
// face rectangle, and whether it crosses strictly between the endpoints.
func segmentFacePoint(a, b m.Vec3, f Face) (m.Vec3, bool) {
	dir := b.Sub(a)
	denom := dir.Dot(f.Normal)
	if denom > -1e-9 && denom < 1e-9 {
		return m.Vec3{}, false
	}
	t := f.P0.Sub(a).Dot(f.Normal) / denom
	if t <= 1e-6 || t >= 1-1e-6 {
		return m.Vec3{}, false
	}
	pt := a.Add(dir.Scale(t))
	if !pointInRect(pt, f) {
		return m.Vec3{}, false
	}
	return pt, true
}

// pointInRect reports whether p (assumed on the face plane) lies within the
// rectangle, by projecting onto the U and V axes.
func pointInRect(p m.Vec3, f Face) bool {
	rel := p.Sub(f.P0)
	uu := f.U.Dot(f.U)
	vv := f.V.Dot(f.V)
	if uu < 1e-12 || vv < 1e-12 {
		return false
	}
	s := rel.Dot(f.U) / uu
	t := rel.Dot(f.V) / vv
	const eps = 1e-6
	return s >= -eps && s <= 1+eps && t >= -eps && t <= 1+eps
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func absf(a float32) float32 {
	if a < 0 {
		return -a
	}
	return a
}

func sqrtf(a float64) float64 { return math.Sqrt(a) }

func clampf64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
