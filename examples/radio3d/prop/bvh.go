package prop

import (
	"sort"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// BVH is a bounding-volume hierarchy over scene faces, used to accelerate the
// many line-of-sight queries the coverage grid issues. A nil *BVH is valid:
// LineOfSight falls back to a brute-force scan, which is fine for a handful of
// buildings. Build one when the query count is large.
type BVH struct {
	faces []Face
	nodes []bvhNode
	order []int // face indices grouped by leaf
}

type bvhNode struct {
	box         AABB
	left, right int // child node indices, or -1 for a leaf
	start, n    int // face range in order[] for a leaf
}

// BuildBVH constructs a median-split BVH over the faces. The returned tree
// references the faces slice; do not mutate it afterward.
func BuildBVH(faces []Face) *BVH {
	b := &BVH{faces: faces}
	if len(faces) == 0 {
		return b
	}
	b.order = make([]int, len(faces))
	for i := range faces {
		b.order[i] = i
	}
	boxes := make([]AABB, len(faces))
	for i, f := range faces {
		boxes[i] = f.AABB()
	}
	b.build(boxes, 0, len(faces))
	return b
}

// build recursively partitions order[start:end], appending nodes; returns the
// index of the created node.
func (b *BVH) build(boxes []AABB, start, end int) int {
	idx := len(b.nodes)
	b.nodes = append(b.nodes, bvhNode{left: -1, right: -1})

	bb := boxes[b.order[start]]
	for i := start + 1; i < end; i++ {
		bb = bb.Union(boxes[b.order[i]])
	}

	count := end - start
	if count <= 2 {
		b.nodes[idx].box = bb
		b.nodes[idx].start = start
		b.nodes[idx].n = count
		return idx
	}

	// Split along the largest extent axis at the median centroid.
	ext := m.Vec3{X: bb.Max.X - bb.Min.X, Y: bb.Max.Y - bb.Min.Y, Z: bb.Max.Z - bb.Min.Z}
	axis := 0
	if ext.Y > ext.X {
		axis = 1
	}
	if ext.Z > ext.X && ext.Z > ext.Y {
		axis = 2
	}
	sub := b.order[start:end]
	sort.SliceStable(sub, func(i, j int) bool {
		return axisVal(boxes[sub[i]].Center(), axis) < axisVal(boxes[sub[j]].Center(), axis)
	})
	mid := start + count/2

	left := b.build(boxes, start, mid)
	right := b.build(boxes, mid, end)
	b.nodes[idx].box = bb
	b.nodes[idx].left = left
	b.nodes[idx].right = right
	return idx
}

func axisVal(v m.Vec3, axis int) float32 {
	switch axis {
	case 1:
		return v.Y
	case 2:
		return v.Z
	default:
		return v.X
	}
}

// segmentOccluded reports whether the open segment a→b is blocked by any face,
// ignoring intersections within eps of either endpoint (so a path leg that starts
// or ends on a reflecting face is not self-blocked).
func (b *BVH) segmentOccluded(a, bp m.Vec3, eps float32) bool {
	if b == nil || len(b.nodes) == 0 {
		return false
	}
	dir := bp.Sub(a)
	return b.occludedNode(0, a, bp, dir, eps)
}

func (b *BVH) occludedNode(node int, a, bp, dir m.Vec3, eps float32) bool {
	n := b.nodes[node]
	if !segmentAABB(a, bp, n.box) {
		return false
	}
	if n.left < 0 { // leaf
		for i := n.start; i < n.start+n.n; i++ {
			if segmentBlocks(a, bp, b.faces[b.order[i]], eps) {
				return true
			}
		}
		return false
	}
	return b.occludedNode(n.left, a, bp, dir, eps) || b.occludedNode(n.right, a, bp, dir, eps)
}

// LineOfSight reports whether the segment a→b is unobstructed by any face,
// ignoring grazes within a small epsilon of the endpoints (so endpoints lying on
// reflecting faces or the ground don't count as blockers). Uses bvh when
// non-nil, else a brute-force scan of faces.
func LineOfSight(a, b m.Vec3, faces []Face, bvh *BVH) bool {
	const eps = 1e-4
	if bvh != nil {
		return !bvh.segmentOccluded(a, b, eps)
	}
	for i := range faces {
		if segmentBlocks(a, b, faces[i], eps) {
			return false
		}
	}
	return true
}

// segmentBlocks reports whether the segment a→b crosses face f at a parameter
// strictly inside (eps, 1-eps).
func segmentBlocks(a, b m.Vec3, f Face, eps float32) bool {
	dir := b.Sub(a)
	denom := dir.Dot(f.Normal)
	if denom > -1e-9 && denom < 1e-9 {
		return false
	}
	t := f.P0.Sub(a).Dot(f.Normal) / denom
	if t <= eps || t >= 1-eps {
		return false
	}
	return pointInRect(a.Add(dir.Scale(t)), f)
}

// segmentAABB is a slab test: does the segment a→b intersect box bb?
func segmentAABB(a, b m.Vec3, bb AABB) bool {
	dir := b.Sub(a)
	tmin := float32(0)
	tmax := float32(1)
	axes := [3]struct{ o, d, lo, hi float32 }{
		{a.X, dir.X, bb.Min.X, bb.Max.X},
		{a.Y, dir.Y, bb.Min.Y, bb.Max.Y},
		{a.Z, dir.Z, bb.Min.Z, bb.Max.Z},
	}
	for _, ax := range axes {
		if absf(ax.d) < 1e-9 {
			if ax.o < ax.lo || ax.o > ax.hi {
				return false
			}
			continue
		}
		inv := 1 / ax.d
		t1 := (ax.lo - ax.o) * inv
		t2 := (ax.hi - ax.o) * inv
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		if t1 > tmin {
			tmin = t1
		}
		if t2 < tmax {
			tmax = t2
		}
		if tmin > tmax {
			return false
		}
	}
	return true
}
