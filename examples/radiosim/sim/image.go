package sim

import (
	"math"
	"math/cmplx"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// ImageEngine is the exact image-method propagation engine: it enumerates the
// direct line-of-sight path and all specular reflections up to Config.MaxOrder by
// mirroring the source across ordered face sequences. It is exact for planar
// surfaces but combinatorial in the face count and reflection order, so it is the
// accuracy oracle the closed-form tests check against — not the real-time engine.
type ImageEngine struct{}

// Paths returns every direct and specularly-reflected path from tx to rx.
func (ImageEngine) Paths(tx Tx, rx Rx, sc *Scene, cfg Config) []Path {
	return propagatePaths(tx, rx.Pos, sc, cfg, newInteraction(sc, cfg))
}

// Coverage evaluates received power (watts) at every grid cell from tx, including
// direct and reflected paths. The result is a flat NX*NZ field indexed by
// Grid.Index, ready to drive a heatmap. Cells coincident with the transmitter are
// skipped (left at 0).
func (e ImageEngine) Coverage(tx Tx, grid Grid, sc *Scene, cfg Config) []float64 {
	inter := newInteraction(sc, cfg)
	field := make([]float64, grid.Len())
	for iz := 0; iz < grid.NZ; iz++ {
		for ix := 0; ix < grid.NX; ix++ {
			p := grid.Cell(ix, iz)
			if p.Sub(tx.Pos).LengthSq() < 1e-4 {
				continue
			}
			paths := propagatePaths(tx, p, sc, cfg, inter)
			pw, _ := Received(paths)
			field[grid.Index(ix, iz)] = pw
		}
	}
	return field
}

// Propagate enumerates the direct path and specular reflections (up to
// cfg.MaxOrder) from tx to field point p through the faces, returning one Path per
// validated route. It is a convenience wrapper that builds a transient scene (all
// faces default material) and the standard interaction; engines call
// propagatePaths directly with a fully-described scene.
func Propagate(tx Tx, p m.Vec3, faces []Face, bvh *BVH, cfg Config) []Path {
	sc := &Scene{Faces: faces, BVH: bvh}
	return propagatePaths(tx, p, sc, cfg, newInteraction(sc, cfg))
}

// propagatePaths is the shared image-method core: it returns the direct path and
// every validated specular reflection, with each path's complex amplitude built
// from the per-bounce interaction physics.
func propagatePaths(tx Tx, p m.Vec3, sc *Scene, cfg Config, inter Interaction) []Path {
	lambda := tx.Wavelength()
	if lambda <= 0 {
		return nil
	}
	k := 2 * math.Pi / lambda
	sqrtPt := math.Sqrt(math.Max(tx.PowerW, 0))

	var paths []Path

	// Direct path. With transmission enabled the path is attenuated by the slab
	// transmission of each wall it crosses (and survives even when blocked);
	// otherwise it requires a clear line of sight.
	L := float64(p.Sub(tx.Pos).Length())
	if L > 1e-6 {
		if cfg.Transmission {
			if g := transmissionGain(tx.Pos, p, sc, inter, tx.FreqHz); cmplx.Abs(g) > 1e-7 {
				paths = append(paths, makePath([]m.Vec3{tx.Pos, p}, L, 0, sqrtPt, lambda, k, g))
			}
		} else if LineOfSight(tx.Pos, p, sc.Faces, sc.BVH) {
			paths = append(paths, makePath([]m.Vec3{tx.Pos, p}, L, 0, sqrtPt, lambda, k, complex(1, 0)))
		}
	}

	// Reflections, order 1..MaxOrder, over ordered face sequences.
	if cfg.MaxOrder >= 1 {
		enumerateReflections(tx, p, sc, cfg, inter, 1, nil, &paths, sqrtPt, lambda, k)
	}

	// Single-edge diffraction fills the shadow behind buildings.
	if cfg.Diffraction {
		diffractionPaths(tx, p, sc, &paths, sqrtPt, lambda, k)
	}

	// Atmospheric absorption attenuates every path by its length.
	applyAtmosphere(paths, sc.Weather, tx.FreqHz)
	return paths
}

// applyAtmosphere attenuates each path's amplitude by the atmospheric field loss
// over its length, given the scene weather and carrier frequency. It is a no-op
// when the specific attenuation is negligible (e.g. sub-6 GHz clear air).
func applyAtmosphere(paths []Path, w Weather, fHz float64) {
	for i := range paths {
		f := atmosphericFieldFactor(w, fHz, paths[i].Length)
		if f != 1 {
			paths[i].Amp *= complex(f, 0)
		}
	}
}

// orderDiffraction marks a Path produced by edge diffraction (Order = -1) so the
// visualization can distinguish it from direct (0) and reflected (≥1) paths.
const orderDiffraction = -1

// diffractionPaths appends one knife-edge diffracted path per edge that bends the
// signal from tx into a shadowed field point p. A diffracted path is added only
// when the direct line of sight is blocked (otherwise the direct/reflected paths
// already carry the field) and both legs tx→Q and Q→p are clear. The amplitude
// is the free-space field at the direct distance scaled by the knife-edge
// coefficient F(v); the polyline bends through the diffraction point Q.
func diffractionPaths(tx Tx, p m.Vec3, sc *Scene, out *[]Path, sqrtPt, lambda, k float64) {
	// Diffraction is the energy path only where the direct ray is obstructed.
	if LineOfSight(tx.Pos, p, sc.Faces, sc.BVH) {
		return
	}
	d0 := float64(p.Sub(tx.Pos).Length())
	if d0 < 1e-6 {
		return
	}
	for _, e := range sc.Edges {
		q, ok := diffractionPoint(e, tx.Pos, p)
		if !ok {
			continue
		}
		if !LineOfSight(tx.Pos, q, sc.Faces, sc.BVH) || !LineOfSight(q, p, sc.Faces, sc.BVH) {
			continue
		}
		sPrime := float64(tx.Pos.Sub(q).Length())
		s := float64(p.Sub(q).Length())
		L := sPrime + s
		// Fresnel parameter from the excess path length; v>0 in the shadow.
		v := 2 * math.Sqrt(math.Max(L-d0, 0)/lambda)
		fd := KnifeEdge(v)
		mag := sqrtPt * lambda / (4 * math.Pi * d0)
		amp := complex(mag, 0) * fd * cmplx.Exp(complex(0, -k*d0))
		*out = append(*out, Path{
			Length: float32(L),
			Delay:  float32(L / SpeedOfLight),
			Amp:    amp,
			Points: []m.Vec3{tx.Pos, q, p},
			Order:  orderDiffraction,
		})
	}
}

// enumerateReflections recurses over ordered face sequences building reflection
// paths. seq is the faces chosen so far; at each depth it both validates the
// current sequence as a path and, if depth < MaxOrder, extends it.
func enumerateReflections(tx Tx, rxPos m.Vec3, sc *Scene, cfg Config, inter Interaction,
	depth int, seq []int, out *[]Path, sqrtPt, lambda, k float64) {

	for fi := range sc.Faces {
		// Don't reflect off the same face twice in a row (degenerate).
		if len(seq) > 0 && seq[len(seq)-1] == fi {
			continue
		}
		cur := append(seq, fi)
		if pts, L, ok := imagePath(tx.Pos, rxPos, cur, sc.Faces, sc.BVH); ok {
			gain := reflectionGain(pts, cur, inter, tx.FreqHz)
			*out = append(*out, makePath(pts, float64(L), len(cur), sqrtPt, lambda, k, gain))
		}
		if depth < cfg.MaxOrder {
			enumerateReflections(tx, rxPos, sc, cfg, inter, depth+1, cur, out, sqrtPt, lambda, k)
		}
		// cur shares seq's backing array; truncate by not retaining it.
		_ = cur
	}
}

// reflectionGain is the product of the per-bounce complex reflection gains along
// a validated reflection polyline pts (Tx, reflection points…, Rx) whose bounces
// occur on the faces in seq. The image method's geometry is already fixed, so we
// take only the gain from each interaction.
func reflectionGain(pts []m.Vec3, seq []int, inter Interaction, fHz float64) complex128 {
	gain := complex(1, 0)
	for i, fi := range seq {
		in := pts[i+1].Sub(pts[i]).Normalize()
		g, _ := inter.OnReflect(fi, pts[i+1], in, fHz, TE)
		gain *= g
	}
	return gain
}

// transmissionGain is the product of the slab transmission gains of every wall
// face the segment a→b crosses. A clear segment crosses nothing and returns 1.
// Each face of a building box counts as one wall, so a ray through a building
// (entry + exit faces) accrues two wall transmissions.
func transmissionGain(a, b m.Vec3, sc *Scene, inter Interaction, fHz float64) complex128 {
	dir := b.Sub(a).Normalize()
	gain := complex(1, 0)
	for fi := range sc.Faces {
		if q, ok := segmentFacePoint(a, b, sc.Faces[fi]); ok {
			g, _ := inter.OnTransmit(fi, q, dir, fHz, TE)
			gain *= g
			if cmplx.Abs(gain) < 1e-7 {
				break
			}
		}
	}
	return gain
}

// imagePath validates a reflection path tx → faces[seq...] → rx via the image
// method: mirror the source successively, then trace reflection points back from
// the receiver, requiring each to land inside its face and each leg to be
// unobstructed. Returns the polyline and total length.
func imagePath(txPos, rxPos m.Vec3, seq []int, faces []Face, bvh *BVH) ([]m.Vec3, float32, bool) {
	// Successive images of the source across each face in order.
	images := make([]m.Vec3, len(seq))
	cur := txPos
	for i, fi := range seq {
		cur = mirror(cur, faces[fi])
		images[i] = cur
	}

	// Trace reflection points from the receiver backward.
	pts := make([]m.Vec3, len(seq)+2)
	pts[len(pts)-1] = rxPos
	target := rxPos
	for i := len(seq) - 1; i >= 0; i-- {
		q, ok := segmentFacePoint(images[i], target, faces[seq[i]])
		if !ok {
			return nil, 0, false
		}
		pts[i+1] = q
		target = q
	}
	pts[0] = txPos

	// Each leg must be clear (endpoints on reflecting faces are excluded by the
	// LineOfSight endpoint epsilon).
	var length float32
	for i := 0; i+1 < len(pts); i++ {
		if !LineOfSight(pts[i], pts[i+1], faces, bvh) {
			return nil, 0, false
		}
		length += pts[i+1].Sub(pts[i]).Length()
	}
	return pts, length, true
}

// makePath builds a Path with its complex amplitude from the geometry and the
// product of the interaction gains. The Friis amplitude form gives magnitude
// sqrt(Pt)·λ/(4π L); the interaction gain carries reflection/transmission
// coefficients; the phase is e^{-jkL}.
func makePath(points []m.Vec3, L float64, order int, sqrtPt, lambda, k float64, gain complex128) Path {
	mag := sqrtPt * lambda / (4 * math.Pi * L)
	amp := complex(mag, 0) * gain * cmplx.Exp(complex(0, -k*L))
	return Path{
		Length: float32(L),
		Delay:  float32(L / SpeedOfLight),
		Amp:    amp,
		Points: points,
		Order:  order,
	}
}
