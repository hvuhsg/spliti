package main

import (
	"math"

	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// Multipath: besides the direct line of sight, a transmitter also reaches a point
// by bouncing off the vertical faces of the wall blocks. Each bounce is a delayed,
// attenuated, phase-shifted copy of the wave, and their coherent sum is what
// produces standing-wave interference on the ground, a spread/rotated receiver
// constellation, and the spatial fading you see when you drag the receiver a few
// wavelengths. The reflections are found by the exact image method in the ground
// (XZ) plane at antenna height, mirroring the source across ordered face sequences
// — the same model block.go already uses for line-of-sight, reusing its blockBox,
// segHitsBox, and losAmp shadowing.

// maxRefl caps the reflection order the image method enumerates, bounding both the
// combinatorial face-sequence count and the fixed-size reflection-point buffers.
const maxRefl = 3

// p2 is a point in the ground plane. All multipath geometry is 2D at antenna
// height, matching the line-of-sight model.
type p2 struct{ x, z float64 }

func dist(a, b p2) float64 { return math.Hypot(a.x-b.x, a.z-b.z) }

func vec3At(p p2, y float32) m.Vec3 { return m.Vec3{X: float32(p.x), Y: y, Z: float32(p.z)} }

// face is one vertical side of a wall block — the mirror for a specular bounce.
// axisX true means the face lies on the plane x = p (normal along ±X) and spans
// z ∈ [lo,hi]; axisX false means the plane z = p spanning x ∈ [lo,hi]. nrm is the
// outward normal sign and blk indexes the owning block, so a bounce off a face is
// never counted as shadowed by its own wall.
type face struct {
	axisX  bool
	p      float64
	lo, hi float64
	nrm    float64
	blk    int
}

// blockFaces returns the four vertical faces of every block, each tagged with its
// owning block index.
func blockFaces(blocks []blockBox) []face {
	fs := make([]face, 0, len(blocks)*4)
	for i := range blocks {
		b := blocks[i]
		fs = append(fs,
			face{axisX: true, p: b.cx + b.hx, lo: b.cz - b.hz, hi: b.cz + b.hz, nrm: +1, blk: i},
			face{axisX: true, p: b.cx - b.hx, lo: b.cz - b.hz, hi: b.cz + b.hz, nrm: -1, blk: i},
			face{axisX: false, p: b.cz + b.hz, lo: b.cx - b.hx, hi: b.cx + b.hx, nrm: +1, blk: i},
			face{axisX: false, p: b.cz - b.hz, lo: b.cx - b.hx, hi: b.cx + b.hx, nrm: -1, blk: i},
		)
	}
	return fs
}

// mirror reflects p across f's plane.
func (f face) mirror(p p2) p2 {
	if f.axisX {
		return p2{2*f.p - p.x, p.z}
	}
	return p2{p.x, 2*f.p - p.z}
}

// outer reports whether p lies on the reflective (outward) side of f.
func (f face) outer(p p2) bool {
	if f.axisX {
		return (p.x-f.p)*f.nrm > 0
	}
	return (p.z-f.p)*f.nrm > 0
}

// cross returns where segment a→b crosses f's plane and whether that crossing is a
// real interior hit: strictly between the endpoints and within the face's finite
// span.
func (f face) cross(a, b p2) (p2, bool) {
	var t float64
	if f.axisX {
		d := b.x - a.x
		if math.Abs(d) < 1e-12 {
			return p2{}, false
		}
		t = (f.p - a.x) / d
	} else {
		d := b.z - a.z
		if math.Abs(d) < 1e-12 {
			return p2{}, false
		}
		t = (f.p - a.z) / d
	}
	if t <= 0 || t >= 1 {
		return p2{}, false
	}
	pt := p2{a.x + t*(b.x-a.x), a.z + t*(b.z-a.z)}
	span := pt.z
	if !f.axisX {
		span = pt.x
	}
	if span < f.lo || span > f.hi {
		return p2{}, false
	}
	return pt, true
}

// imgNode is a precomputed virtual source: the transmitter mirrored through a
// sequence of faces. imgs[k] is the transmitter mirrored through faces[0..k], so
// imgs[len-1] is the virtual source seen past the whole bounce sequence. The node
// depends only on the transmitter position and the faces — not the destination —
// so the tree is built once per transmitter per frame and reused across every
// ground cell and the receiver.
type imgNode struct {
	faces []face
	imgs  []p2
}

// buildImages enumerates every face sequence up to maxOrder (never the same face
// twice in a row) and the chain of mirrored images for each, starting from the
// transmitter at tx. Independent of any destination, so the result is shared
// across all field points.
func buildImages(tx p2, faces []face, maxOrder int) []imgNode {
	if maxOrder > maxRefl {
		maxOrder = maxRefl
	}
	if maxOrder < 1 || len(faces) == 0 {
		return nil
	}
	var nodes []imgNode
	frontier := make([]imgNode, 0, len(faces))
	for _, f := range faces {
		frontier = append(frontier, imgNode{faces: []face{f}, imgs: []p2{f.mirror(tx)}})
	}
	nodes = append(nodes, frontier...)
	for order := 2; order <= maxOrder; order++ {
		var next []imgNode
		for _, n := range frontier {
			last := n.faces[len(n.faces)-1]
			for _, f := range faces {
				if f == last {
					continue // no immediate re-reflection off the same face
				}
				nf := append(append([]face{}, n.faces...), f)
				ni := append(append([]p2{}, n.imgs...), f.mirror(n.imgs[len(n.imgs)-1]))
				next = append(next, imgNode{faces: nf, imgs: ni})
			}
		}
		nodes = append(nodes, next...)
		frontier = next
	}
	return nodes
}

// rfTap is the channel contribution of one path: its total geometric length (which
// sets the carrier phase and the symbol delay) and its real amplitude attenuation
// (per-leg line-of-sight shadowing times the reflection coefficient once per
// bounce). order is the bounce count (0 = direct).
type rfTap struct {
	length, atten float64
	order         int
}

// validatePath reconstructs the reflection points for one image node against dst,
// returning the bounce order, per-leg attenuation, total length, and the points.
// It allocates nothing — reflection points live in a fixed stack array — so it is
// safe to call per ground cell. ok is false when the geometry is invalid (a
// reflection misses its face span or the endpoints are on the wrong side).
func validatePath(n *imgNode, tx, dst p2, freqHz float64, blocks []blockBox, refl float64) (pts [maxRefl]p2, order int, atten, length float64, ok bool) {
	order = len(n.faces)
	// Walk back from dst through the bounce sequence, recovering each reflection
	// point: P_k lies on faces[k] and on the line from image imgs[k] to the next
	// point downstream.
	next := dst
	for k := order - 1; k >= 0; k-- {
		pt, hit := n.faces[k].cross(n.imgs[k], next)
		if !hit {
			return pts, order, 0, 0, false
		}
		pts[k] = pt
		next = pt
	}
	if !n.faces[0].outer(tx) || !n.faces[order-1].outer(dst) {
		return pts, order, 0, 0, false
	}
	// Reflection loss once per bounce, then per-leg shadowing by *other* walls (the
	// walls this leg starts/ends on are excluded so a face never shadows its own
	// bounce).
	atten = math.Pow(refl, float64(order))
	prev := tx
	for k := 0; k <= order; k++ {
		b := dst
		e1 := -1
		if k < order {
			b = pts[k]
			e1 = n.faces[k].blk
		}
		e0 := -1
		if k >= 1 {
			e0 = n.faces[k-1].blk
		}
		atten *= losAmpExcl(prev.x, prev.z, b.x, b.z, freqHz, blocks, e0, e1)
		prev = b
	}
	length = dist(n.imgs[order-1], dst)
	return pts, order, atten, length, true
}

// collectReflections appends one rfTap per geometrically valid specular path from
// tx to dst into *taps (which the caller resets and reuses, so the hot ground-cell
// loop allocates nothing). refl is the per-bounce amplitude coefficient; maxOrder
// caps the bounces considered (≤ the order the image tree was built for).
func collectReflections(tx, dst p2, freqHz float64, blocks []blockBox, nodes []imgNode, refl float64, maxOrder int, taps *[]rfTap) {
	for ni := range nodes {
		n := &nodes[ni]
		if len(n.faces) > maxOrder {
			continue
		}
		_, order, atten, length, ok := validatePath(n, tx, dst, freqHz, blocks, refl)
		if !ok {
			continue
		}
		*taps = append(*taps, rfTap{length: length, atten: atten, order: order})
	}
}

// rfPath is one route with its drawable polyline {tx, refl₁, …, dst} at height y,
// for the reflected-ray overlay. Built only for the focused pair (one destination),
// so the allocation is negligible.
type rfPath struct {
	length, atten float64
	order         int
	pts           []m.Vec3
}

// reflectionRays returns the drawable polyline for every valid specular path from
// tx to dst, lifted to height y so the rays read above the ground field.
func reflectionRays(tx, dst p2, y float32, freqHz float64, blocks []blockBox, nodes []imgNode, refl float64, maxOrder int) []rfPath {
	var out []rfPath
	for ni := range nodes {
		n := &nodes[ni]
		if len(n.faces) > maxOrder {
			continue
		}
		pts, order, atten, length, ok := validatePath(n, tx, dst, freqHz, blocks, refl)
		if !ok {
			continue
		}
		poly := make([]m.Vec3, 0, order+2)
		poly = append(poly, vec3At(tx, y))
		for k := 0; k < order; k++ {
			poly = append(poly, vec3At(pts[k], y))
		}
		poly = append(poly, vec3At(dst, y))
		out = append(out, rfPath{length: length, atten: atten, order: order, pts: poly})
	}
	return out
}

// losAmpExcl is losAmp (block.go) with up to two block indices excluded from the
// shadowing test — the walls a reflected leg starts or ends on, which it grazes by
// construction and must not be shadowed by.
func losAmpExcl(ax, az, bx, bz, freqHz float64, blocks []blockBox, e0, e1 int) float64 {
	if freqHz <= 0 || len(blocks) == 0 {
		return 1
	}
	lambda := sim.SpeedOfLight / freqHz
	a := 1.0
	for i := range blocks {
		if i == e0 || i == e1 {
			continue
		}
		a *= edgeDiffraction(ax, az, bx, bz, lambda, blocks[i])
		if a <= shadowFloor {
			return shadowFloor
		}
	}
	return a
}
