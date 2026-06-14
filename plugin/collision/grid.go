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
	// pool recycles per-cell id slices across reset cycles so that repeated
	// per-tick rebuilds (see broadphase2) do not allocate a fresh slice per
	// occupied cell every frame.
	pool [][]int
}

// NewGrid2 returns an empty grid whose cells are cell units on a side. A
// non-positive cell is clamped to 1.
func NewGrid2(cell int) *Grid2 {
	if cell <= 0 {
		cell = 1
	}
	return &Grid2{cell: cell, cells: map[[2]int][]int{}}
}

// reset clears the grid for reuse with a new cell size, recycling every
// occupied cell's id slice into the pool (truncated, capacity retained) and
// keeping the map's bucket allocation. After reset the grid holds no entries.
func (g *Grid2) reset(cell int) {
	if cell <= 0 {
		cell = 1
	}
	g.cell = cell
	if g.cells == nil {
		g.cells = map[[2]int][]int{}
		return
	}
	for k, s := range g.cells {
		g.pool = append(g.pool, s[:0])
		delete(g.cells, k)
	}
}

// takeSlice returns a recycled id slice (length 0) from the pool, or nil when
// the pool is empty so the first append allocates.
func (g *Grid2) takeSlice() []int {
	n := len(g.pool)
	if n == 0 {
		return nil
	}
	s := g.pool[n-1]
	g.pool = g.pool[:n-1]
	return s
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
			s, ok := g.cells[k]
			if !ok {
				s = g.takeSlice()
			}
			g.cells[k] = append(s, id)
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

// broadphase2 holds reusable scratch (grid, mark, candidate slices) for the 2D
// broad phase so that a system running it every tick does not reallocate the
// grid map and temporaries each frame. The zero value is ready to use; reuse
// the same instance across ticks to amortize allocation.
type broadphase2 struct {
	grid Grid2
	mark []int
	raw  []int
	cand []int
}

// pairs invokes emit(i, j) for every body pair (i<j) whose boxes overlap and
// whose layers permit a collision. Pairs are emitted in ascending (i, then j)
// order — identical to a brute-force i<j scan — so the event stream is
// deterministic regardless of the spatial hash's internal bucketing.
func (bp *broadphase2) pairs(bodies []aabb2, cell int, emit func(i, j int)) {
	if len(bodies) < 2 {
		return
	}
	bp.grid.reset(cell)
	for i := range bodies {
		b := bodies[i]
		bp.grid.Insert(i, b.minX, b.minY, b.maxX, b.maxY)
	}
	// mark[j] == i means body j has already been collected as a candidate of
	// body i this round, so a pair sharing several cells is tested only once.
	bp.mark = resizeInts(bp.mark, len(bodies))
	for i := range bp.mark {
		bp.mark[i] = -1
	}
	raw, cand := bp.raw, bp.cand
	for i := range bodies {
		b := bodies[i]
		raw = bp.grid.candidates(b.minX, b.minY, b.maxX, b.maxY, raw[:0])
		cand = cand[:0]
		for _, j := range raw {
			if j <= i || bp.mark[j] == i {
				continue
			}
			bp.mark[j] = i
			cand = append(cand, j)
		}
		sort.Ints(cand)
		for _, j := range cand {
			if overlap2(b, bodies[j]) && collides(b.layer, b.mask, bodies[j].layer, bodies[j].mask) {
				emit(i, j)
			}
		}
	}
	bp.raw, bp.cand = raw, cand
}

// pairs2 is the allocating one-shot form, kept for tests and callers that do
// not retain broad-phase state. It matches the original free-function behavior.
func pairs2(bodies []aabb2, cell int, emit func(i, j int)) {
	var bp broadphase2
	bp.pairs(bodies, cell, emit)
}

// resizeInts returns s resliced to length n, reusing its backing array when it
// has the capacity and allocating a fresh slice otherwise.
func resizeInts(s []int, n int) []int {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]int, n)
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
	pool  [][]int
}

// NewGrid3 returns an empty 3D grid with cubic cells of the given side. A
// non-positive cell is clamped to 1.
func NewGrid3(cell float32) *Grid3 {
	if cell <= 0 {
		cell = 1
	}
	return &Grid3{cell: cell, cells: map[[3]int][]int{}}
}

// reset clears the grid for reuse, recycling cell slices into the pool. See
// Grid2.reset.
func (g *Grid3) reset(cell float32) {
	if cell <= 0 {
		cell = 1
	}
	g.cell = cell
	if g.cells == nil {
		g.cells = map[[3]int][]int{}
		return
	}
	for k, s := range g.cells {
		g.pool = append(g.pool, s[:0])
		delete(g.cells, k)
	}
}

func (g *Grid3) takeSlice() []int {
	n := len(g.pool)
	if n == 0 {
		return nil
	}
	s := g.pool[n-1]
	g.pool = g.pool[:n-1]
	return s
}

// Insert records id in every cell the box [min,max] overlaps.
func (g *Grid3) Insert(id int, min, max m.Vec3) {
	cx0, cy0, cz0 := cellOf(min.X, g.cell), cellOf(min.Y, g.cell), cellOf(min.Z, g.cell)
	cx1, cy1, cz1 := cellOf(max.X, g.cell), cellOf(max.Y, g.cell), cellOf(max.Z, g.cell)
	for cz := cz0; cz <= cz1; cz++ {
		for cy := cy0; cy <= cy1; cy++ {
			for cx := cx0; cx <= cx1; cx++ {
				k := [3]int{cx, cy, cz}
				s, ok := g.cells[k]
				if !ok {
					s = g.takeSlice()
				}
				g.cells[k] = append(s, id)
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

// broadphase3 is the 3D analogue of broadphase2.
type broadphase3 struct {
	grid Grid3
	mark []int
	raw  []int
	cand []int
}

// pairs is the 3D analogue of broadphase2.pairs.
func (bp *broadphase3) pairs(bodies []aabb3, cell float32, emit func(i, j int)) {
	if len(bodies) < 2 {
		return
	}
	bp.grid.reset(cell)
	for i := range bodies {
		bp.grid.Insert(i, bodies[i].min, bodies[i].max)
	}
	bp.mark = resizeInts(bp.mark, len(bodies))
	for i := range bp.mark {
		bp.mark[i] = -1
	}
	raw, cand := bp.raw, bp.cand
	for i := range bodies {
		b := bodies[i]
		raw = bp.grid.candidates(b.min, b.max, raw[:0])
		cand = cand[:0]
		for _, j := range raw {
			if j <= i || bp.mark[j] == i {
				continue
			}
			bp.mark[j] = i
			cand = append(cand, j)
		}
		sort.Ints(cand)
		for _, j := range cand {
			if overlap3(b, bodies[j]) && collides(b.layer, b.mask, bodies[j].layer, bodies[j].mask) {
				emit(i, j)
			}
		}
	}
	bp.raw, bp.cand = raw, cand
}

// pairs3 is the allocating one-shot form, kept for tests.
func pairs3(bodies []aabb3, cell float32, emit func(i, j int)) {
	var bp broadphase3
	bp.pairs(bodies, cell, emit)
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
