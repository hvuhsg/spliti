package prop

import (
	"math"
	"math/cmplx"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// Tx is a transmitter: a position, a radiated power in watts, and a carrier
// frequency in hertz (which sets the wavelength and per-meter phase rotation).
type Tx struct {
	Pos    m.Vec3
	PowerW float64
	FreqHz float64
}

// Wavelength returns λ = c/f in meters.
func (t Tx) Wavelength() float64 {
	if t.FreqHz <= 0 {
		return 0
	}
	return SpeedOfLight / t.FreqHz
}

// Config tunes the propagation model.
type Config struct {
	// MaxOrder is the highest reflection order to enumerate (0 = direct only,
	// 1 = single-bounce, 2 = double-bounce). Higher orders grow combinatorially,
	// so 1–2 is the real-time range.
	MaxOrder int
	// Reflection is the (real) reflection coefficient Γ applied per bounce. A
	// physical wall is partially absorbing and phase-inverting, so a value like
	// -0.7 is typical. Each bounce multiplies the amplitude by Γ.
	Reflection float64
}

// DefaultConfig is a reasonable single-bounce setup.
func DefaultConfig() Config { return Config{MaxOrder: 1, Reflection: -0.7} }

// Path is one propagation path from the transmitter to a field point: its total
// length, the delay (length/c), the complex amplitude (magnitude = received field
// strength contribution, phase = carrier phase at arrival), and the polyline of
// world points it travels (Tx, reflection points…, field point) for drawing.
type Path struct {
	Length float32
	Delay  float32
	Amp    complex128
	Points []m.Vec3
	Order  int
}

// Propagate enumerates the direct path and specular reflections (up to
// cfg.MaxOrder) from tx to field point p through the faces, returning one Path per
// validated route. faces and bvh describe the same scene (bvh may be nil). The
// complex amplitudes can be summed (see Received) to get the field at p.
func Propagate(tx Tx, p m.Vec3, faces []Face, bvh *BVH, cfg Config) []Path {
	lambda := tx.Wavelength()
	if lambda <= 0 {
		return nil
	}
	k := 2 * math.Pi / lambda
	sqrtPt := math.Sqrt(math.Max(tx.PowerW, 0))

	var paths []Path

	// Direct path.
	if LineOfSight(tx.Pos, p, faces, bvh) {
		L := float64(p.Sub(tx.Pos).Length())
		if L > 1e-6 {
			paths = append(paths, makePath([]m.Vec3{tx.Pos, p}, L, 0, sqrtPt, lambda, k, cfg.Reflection))
		}
	}

	// Reflections, order 1..MaxOrder, over ordered face sequences.
	if cfg.MaxOrder >= 1 {
		enumerateReflections(tx.Pos, p, faces, bvh, cfg, 1, nil, &paths, sqrtPt, lambda, k)
	}
	return paths
}

// enumerateReflections recurses over ordered face sequences building reflection
// paths. seq is the faces chosen so far; at each depth it both validates the
// current sequence as a path and, if depth < MaxOrder, extends it.
func enumerateReflections(txPos, rxPos m.Vec3, faces []Face, bvh *BVH, cfg Config,
	depth int, seq []int, out *[]Path, sqrtPt, lambda, k float64) {

	for fi := range faces {
		// Don't reflect off the same face twice in a row (degenerate).
		if len(seq) > 0 && seq[len(seq)-1] == fi {
			continue
		}
		cur := append(seq, fi)
		if pts, L, ok := imagePath(txPos, rxPos, cur, faces, bvh); ok {
			*out = append(*out, makePath(pts, float64(L), len(cur), sqrtPt, lambda, k, cfg.Reflection))
		}
		if depth < cfg.MaxOrder {
			enumerateReflections(txPos, rxPos, faces, bvh, cfg, depth+1, cur, out, sqrtPt, lambda, k)
		}
		// cur shares seq's backing array; truncate by not retaining it.
		_ = cur
	}
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

// makePath builds a Path with its complex amplitude from the geometry. The
// Friis amplitude form gives magnitude sqrt(Pt)·λ/(4π L); each reflection
// multiplies by Γ; the phase is e^{-jkL}.
func makePath(points []m.Vec3, L float64, order int, sqrtPt, lambda, k, reflection float64) Path {
	mag := sqrtPt * lambda / (4 * math.Pi * L)
	gamma := math.Pow(reflection, float64(order))
	amp := complex(mag*gamma, 0) * cmplx.Exp(complex(0, -k*L))
	return Path{
		Length: float32(L),
		Delay:  float32(L / SpeedOfLight),
		Amp:    amp,
		Points: points,
		Order:  order,
	}
}
