package collision

import (
	"math"
	"sort"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// floorDiv returns floor(a/b) for a positive divisor b. Plain Go integer
// division truncates toward zero, so it is wrong for negative coordinates
// (e.g. -1/16 == 0, but the cell of -1 is -1). This corrects that so bodies at
// negative world positions land in the right cells.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// cellOf returns the grid cell index of a float coordinate.
func cellOf(v, cell float32) int { return int(math.Floor(float64(v / cell))) }

// normLayer maps the zero value of a (Layer, Mask) pair to "default layer,
// collide with everything", so a plain collider that never sets these fields
// behaves exactly as it did before layers existed.
func normLayer(layer, mask uint32) (uint32, uint32) {
	if layer == 0 {
		layer = 1
	}
	if mask == 0 {
		mask = 0xFFFFFFFF
	}
	return layer, mask
}

// collides reports whether two bodies' layer/mask pairs permit a collision.
// A collides with B only if A's mask includes one of B's layers AND vice versa,
// so filtering is symmetric and order-independent.
func collides(aLayer, aMask, bLayer, bMask uint32) bool {
	al, am := normLayer(aLayer, aMask)
	bl, bm := normLayer(bLayer, bMask)
	return am&bl != 0 && bm&al != 0
}

// --- 2D ------------------------------------------------------------------

// aabb2 is an integer axis-aligned box [minX,maxX) × [minY,maxY) with a
// layer/mask filter. The half-open convention means boxes that merely touch
// along a face do not overlap, matching the engine's original collision test.
type aabb2 struct {
	minX, minY  int
	maxX, maxY  int
	layer, mask uint32
}

// overlap2 reports whether two half-open integer boxes intersect.
func overlap2(a, b aabb2) bool {
	if a.maxX <= b.minX || b.maxX <= a.minX {
		return false
	}
	if a.maxY <= b.minY || b.maxY <= a.minY {
		return false
	}
	return true
}

// Grid2 is a 2D uniform spatial hash for broad-phase queries. Insert each
// body's AABB once under an integer id, then Query an AABB to get the ids whose
// cells it shares. The grid is unbounded (cells live in a map) so it needs no
// world extent and handles negative coordinates.
//
// The zero value is not usable; build one with NewGrid2.
type Grid2 struct {
	cell  int
	cells map[[2]int][]int
}

// NewGrid2 returns an empty grid whose cells are cell units on a side. A
// non-positive cell is clamped to 1.
func NewGrid2(cell int) *Grid2 {
	if cell <= 0 {
		cell = 1
	}
	return &Grid2{cell: cell, cells: map[[2]int][]int{}}
}

// Insert records id in every cell the box [minX,maxX) × [minY,maxY) overlaps.
func (g *Grid2) Insert(id, minX, minY, maxX, maxY int) {
	cx0, cy0 := floorDiv(minX, g.cell), floorDiv(minY, g.cell)
	// maxX/maxY are exclusive, so subtract one before flooring — otherwise a
	// box flush against a cell boundary would claim the next, empty cell.
	cx1, cy1 := floorDiv(maxX-1, g.cell), floorDiv(maxY-1, g.cell)
	for cy := cy0; cy <= cy1; cy++ {
		for cx := cx0; cx <= cx1; cx++ {
			k := [2]int{cx, cy}
			g.cells[k] = append(g.cells[k], id)
		}
	}
}

// candidates appends the ids in every cell the box spans to out and returns it.
// The result may contain duplicates (a body spanning several shared cells) and
// is in cell-iteration order; callers dedupe and sort as needed.
func (g *Grid2) candidates(minX, minY, maxX, maxY int, out []int) []int {
	cx0, cy0 := floorDiv(minX, g.cell), floorDiv(minY, g.cell)
	cx1, cy1 := floorDiv(maxX-1, g.cell), floorDiv(maxY-1, g.cell)
	for cy := cy0; cy <= cy1; cy++ {
		for cx := cx0; cx <= cx1; cx++ {
			out = append(out, g.cells[[2]int{cx, cy}]...)
		}
	}
	return out
}

// Query returns the sorted, de-duplicated ids whose cells the box overlaps.
// These are broad-phase candidates: an exact AABB test is still required.
func (g *Grid2) Query(minX, minY, maxX, maxY int) []int {
	raw := g.candidates(minX, minY, maxX, maxY, nil)
	return sortUnique(raw)
}

// pairs2 invokes emit(i, j) for every body pair (i<j) whose boxes overlap and
// whose layers permit a collision. Pairs are emitted in ascending (i, then j)
// order — identical to a brute-force i<j scan — so the event stream is
// deterministic regardless of the spatial hash's internal bucketing.
func pairs2(bodies []aabb2, cell int, emit func(i, j int)) {
	if len(bodies) < 2 {
		return
	}
	g := NewGrid2(cell)
	for i := range bodies {
		b := bodies[i]
		g.Insert(i, b.minX, b.minY, b.maxX, b.maxY)
	}
	// mark[j] == i means body j has already been collected as a candidate of
	// body i this round, so a pair sharing several cells is tested only once.
	mark := make([]int, len(bodies))
	for i := range mark {
		mark[i] = -1
	}
	var raw, cand []int
	for i := range bodies {
		b := bodies[i]
		raw = g.candidates(b.minX, b.minY, b.maxX, b.maxY, raw[:0])
		cand = cand[:0]
		for _, j := range raw {
			if j <= i || mark[j] == i {
				continue
			}
			mark[j] = i
			cand = append(cand, j)
		}
		sort.Ints(cand)
		for _, j := range cand {
			if overlap2(b, bodies[j]) && collides(b.layer, b.mask, bodies[j].layer, bodies[j].mask) {
				emit(i, j)
			}
		}
	}
}

// --- 3D ------------------------------------------------------------------

// aabb3 is a float axis-aligned box [min,max] with a layer/mask filter.
type aabb3 struct {
	min, max    m.Vec3
	layer, mask uint32
}

// overlap3 reports whether two float boxes intersect. Touching faces (equal
// bounds) are treated as non-overlapping, mirroring the 2D convention.
func overlap3(a, b aabb3) bool {
	if a.max.X <= b.min.X || b.max.X <= a.min.X {
		return false
	}
	if a.max.Y <= b.min.Y || b.max.Y <= a.min.Y {
		return false
	}
	if a.max.Z <= b.min.Z || b.max.Z <= a.min.Z {
		return false
	}
	return true
}

// Grid3 is the 3D counterpart of Grid2: a uniform spatial hash over float
// coordinates keyed by integer cell triples.
type Grid3 struct {
	cell  float32
	cells map[[3]int][]int
}

// NewGrid3 returns an empty 3D grid with cubic cells of the given side. A
// non-positive cell is clamped to 1.
func NewGrid3(cell float32) *Grid3 {
	if cell <= 0 {
		cell = 1
	}
	return &Grid3{cell: cell, cells: map[[3]int][]int{}}
}

// Insert records id in every cell the box [min,max] overlaps.
func (g *Grid3) Insert(id int, min, max m.Vec3) {
	cx0, cy0, cz0 := cellOf(min.X, g.cell), cellOf(min.Y, g.cell), cellOf(min.Z, g.cell)
	cx1, cy1, cz1 := cellOf(max.X, g.cell), cellOf(max.Y, g.cell), cellOf(max.Z, g.cell)
	for cz := cz0; cz <= cz1; cz++ {
		for cy := cy0; cy <= cy1; cy++ {
			for cx := cx0; cx <= cx1; cx++ {
				k := [3]int{cx, cy, cz}
				g.cells[k] = append(g.cells[k], id)
			}
		}
	}
}

func (g *Grid3) candidates(min, max m.Vec3, out []int) []int {
	cx0, cy0, cz0 := cellOf(min.X, g.cell), cellOf(min.Y, g.cell), cellOf(min.Z, g.cell)
	cx1, cy1, cz1 := cellOf(max.X, g.cell), cellOf(max.Y, g.cell), cellOf(max.Z, g.cell)
	for cz := cz0; cz <= cz1; cz++ {
		for cy := cy0; cy <= cy1; cy++ {
			for cx := cx0; cx <= cx1; cx++ {
				out = append(out, g.cells[[3]int{cx, cy, cz}]...)
			}
		}
	}
	return out
}

// Query returns the sorted, de-duplicated candidate ids for the box [min,max].
func (g *Grid3) Query(min, max m.Vec3) []int {
	return sortUnique(g.candidates(min, max, nil))
}

// pairs3 is the 3D analogue of pairs2.
func pairs3(bodies []aabb3, cell float32, emit func(i, j int)) {
	if len(bodies) < 2 {
		return
	}
	g := NewGrid3(cell)
	for i := range bodies {
		g.Insert(i, bodies[i].min, bodies[i].max)
	}
	mark := make([]int, len(bodies))
	for i := range mark {
		mark[i] = -1
	}
	var raw, cand []int
	for i := range bodies {
		b := bodies[i]
		raw = g.candidates(b.min, b.max, raw[:0])
		cand = cand[:0]
		for _, j := range raw {
			if j <= i || mark[j] == i {
				continue
			}
			mark[j] = i
			cand = append(cand, j)
		}
		sort.Ints(cand)
		for _, j := range cand {
			if overlap3(b, bodies[j]) && collides(b.layer, b.mask, bodies[j].layer, bodies[j].mask) {
				emit(i, j)
			}
		}
	}
}

// sortUnique sorts ids ascending and removes duplicates in place.
func sortUnique(ids []int) []int {
	if len(ids) < 2 {
		return ids
	}
	sort.Ints(ids)
	out := ids[:1]
	for _, v := range ids[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
