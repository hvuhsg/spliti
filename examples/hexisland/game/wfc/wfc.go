// Package wfc is the wave-function-collapse model for the hex island: the hex
// grid math, the terrain tile catalog with its adjacency rules, and the
// constraint propagation. It has no engine dependencies, so the rules can be
// tested and reasoned about on their own.
//
// The player chooses *where* to place a tile; this package chooses *what* — it
// collapses the picked cell to one of the tiles still allowed by the rules,
// then propagates the new constraint across the board (classic WFC arc
// consistency) so neighbouring cells shrink their own sets of possibilities.
package wfc

import (
	"math"
	"math/rand"
)

// Terrain is the material at a hex tile's rim. The five terrains form a natural
// height gradient — water, sand, grass, dirt, stone — and two tiles may sit
// next to each other only if their terrains are within one step on that
// gradient (see Compatible). That single rule is what makes islands come out
// looking like islands: water rings into beaches, beaches into grass, grass
// climbs to dirt and bare rock.
type Terrain uint8

const (
	Water Terrain = iota
	Sand
	Grass
	Dirt
	Stone
	terrainCount
)

// Compatible reports whether terrains a and b may be adjacent: they must be
// within one step on the water→stone gradient.
func Compatible(a, b Terrain) bool {
	d := int(a) - int(b)
	if d < 0 {
		d = -d
	}
	return d <= 1
}

// TileDef is one model from the Kenney hexagon kit paired with the terrain on
// its rim. Several tiles share a terrain (grass, grass-forest and grass-hill
// are all Grass); that is exactly what gives a collapsed cell more than one
// valid choice, so the same rules can produce a different-looking island each
// time.
type TileDef struct {
	Model   string // base GLB filename without extension, e.g. "grass-forest"
	Terrain Terrain
}

// Tiles is the catalog the game collapses cells to. All tiles of a terrain are
// interchangeable as far as the rules are concerned, so when the game collapses
// a cell it picks among the matching tiles at random.
var Tiles = []TileDef{
	{"water", Water}, {"water-rocks", Water}, {"water-island", Water},
	{"sand", Sand}, {"sand-desert", Sand}, {"sand-rocks", Sand},
	{"grass", Grass}, {"grass-forest", Grass}, {"grass-hill", Grass},
	{"dirt", Dirt}, {"dirt-lumber", Dirt},
	{"stone", Stone}, {"stone-hill", Stone}, {"stone-mountain", Stone}, {"stone-rocks", Stone},
}

const fullDomain = uint8(1<<terrainCount) - 1 // bitmask with every terrain possible

// openWaterOnly lists tiles that, beyond their terrain's gradient rule, may only
// be placed in open water: every neighbour must already be water (a collapsed
// water tile, or the open ocean past the board edge). It's how "an island is
// only allowed when everything around it is water" is enforced — the gradient
// alone would let water-island sit against a beach.
var openWaterOnly = map[string]bool{
	"water-island": true,
}

// Coord is an axial hex coordinate. The grid uses flat-left/right-edge hexes
// (the Kenney kit's orientation), so +Q steps east and +R steps north-east.
type Coord struct{ Q, R int }

// axialDirs are the six neighbour offsets in axial space, one per hex edge.
var axialDirs = [6]Coord{
	{1, 0}, {1, -1}, {0, -1}, {-1, 0}, {-1, 1}, {0, 1},
}

// Cell is one hex slot in the board's wave: Domain is the set of terrains still
// possible (a bitmask, one bit per Terrain). Once the cell collapses, Tile is
// the chosen index into Tiles and Domain holds only that tile's terrain.
type Cell struct {
	Domain    uint8
	Collapsed bool
	Tile      int
}

// Board is the radius-N hexagonal play area together with its wave state.
type Board struct {
	Radius       int
	Cells        map[Coord]*Cell
	anyCollapsed bool
}

// NewBoard builds an empty radius-N hex board: every in-bounds cell starts with
// the full set of terrains possible. A cell is in bounds when its cube
// coordinates (q, r, -q-r) are all within ±radius.
func NewBoard(radius int) *Board {
	b := &Board{Radius: radius, Cells: map[Coord]*Cell{}}
	for q := -radius; q <= radius; q++ {
		for r := -radius; r <= radius; r++ {
			if abs(q) <= radius && abs(r) <= radius && abs(q+r) <= radius {
				b.Cells[Coord{q, r}] = &Cell{Domain: fullDomain}
			}
		}
	}
	return b
}

// Placeable reports whether the player may build on coord right now: it must be
// an in-bounds, uncollapsed cell that still has at least one possible terrain,
// and — once the island has started — it must touch an already-placed tile so
// the island grows outward rather than scattering.
func (b *Board) Placeable(coord Coord) bool {
	cell := b.Cells[coord]
	if cell == nil || cell.Collapsed || cell.Domain == 0 {
		return false
	}
	if !b.anyCollapsed {
		return true // first tile may go anywhere
	}
	for _, d := range axialDirs {
		if n := b.Cells[Coord{coord.Q + d.Q, coord.R + d.R}]; n != nil && n.Collapsed {
			return true
		}
	}
	return false
}

// Collapse fixes coord to a concrete tile chosen at random among those the
// rules still allow, then propagates the constraint across the board. It
// returns the chosen tile and true on success, or ok=false if the cell can't be
// collapsed (already filled, out of bounds, or its possibilities ran dry).
func (b *Board) Collapse(coord Coord, rng *rand.Rand) (TileDef, bool) {
	cell := b.Cells[coord]
	if cell == nil || cell.Collapsed || cell.Domain == 0 {
		return TileDef{}, false
	}
	// The domain has already been narrowed by propagation to the terrains
	// compatible with every collapsed neighbour, so any tile whose terrain is
	// still in the domain is a legal choice here.
	var cands []int
	for i, t := range Tiles {
		if cell.Domain&(1<<t.Terrain) == 0 {
			continue
		}
		if openWaterOnly[t.Model] && !b.allNeighboursWater(coord) {
			continue // e.g. an island may only sit in open water
		}
		cands = append(cands, i)
	}
	if len(cands) == 0 {
		return TileDef{}, false
	}
	pick := cands[rng.Intn(len(cands))]
	cell.Tile = pick
	cell.Collapsed = true
	cell.Domain = 1 << Tiles[pick].Terrain
	b.anyCollapsed = true
	b.propagate(coord)
	return Tiles[pick], true
}

// propagate runs arc consistency outward from a cell whose domain just changed:
// for each neighbour, drop any terrain that no longer has a compatible partner
// in this cell's domain, and keep cascading wherever a domain shrinks. Collapsed
// cells are fixed and only provide support; they are never narrowed.
func (b *Board) propagate(seed Coord) {
	queue := []Coord{seed}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		curDom := b.Cells[cur].Domain
		for _, d := range axialDirs {
			nc := Coord{cur.Q + d.Q, cur.R + d.R}
			cell := b.Cells[nc]
			if cell == nil || cell.Collapsed {
				continue
			}
			newDom := cell.Domain
			for t := Terrain(0); t < terrainCount; t++ {
				bit := uint8(1) << t
				if newDom&bit != 0 && !supported(t, curDom) {
					newDom &^= bit
				}
			}
			if newDom != cell.Domain {
				cell.Domain = newDom
				queue = append(queue, nc)
			}
		}
	}
}

// allNeighboursWater reports whether every neighbour of coord is water: a
// collapsed water tile, or off the board (treated as open ocean). An in-bounds
// neighbour that is still empty or any non-water tile makes it false.
func (b *Board) allNeighboursWater(coord Coord) bool {
	for _, d := range axialDirs {
		cell := b.Cells[Coord{coord.Q + d.Q, coord.R + d.R}]
		if cell == nil {
			continue // off the board = open ocean
		}
		if !cell.Collapsed || Tiles[cell.Tile].Terrain != Water {
			return false
		}
	}
	return true
}

// supported reports whether terrain t has at least one compatible terrain among
// those still possible in dom.
func supported(t Terrain, dom uint8) bool {
	for u := Terrain(0); u < terrainCount; u++ {
		if dom&(1<<u) != 0 && Compatible(t, u) {
			return true
		}
	}
	return false
}

const sqrt3over2 = 0.86602540378443864

// WorldXZ maps an axial coordinate to a world-space (x, z) position. Tiles are
// unit hexes (apothem 0.5), so neighbours land exactly one tile apart.
func (b *Board) WorldXZ(coord Coord) (float32, float32) {
	x := float64(coord.Q) + float64(coord.R)*0.5
	z := float64(coord.R) * sqrt3over2
	return float32(x), float32(z)
}

// FromWorld maps a world-space (x, z) point back to the nearest hex via cube
// rounding — the inverse of WorldXZ, used to turn a cursor ray hit into a cell.
func FromWorld(x, z float32) Coord {
	rf := float64(z) / sqrt3over2
	qf := float64(x) - rf*0.5
	return cubeRound(qf, rf)
}

func cubeRound(qf, rf float64) Coord {
	xf, zf := qf, rf
	yf := -xf - zf
	rx, ry, rz := math.Round(xf), math.Round(yf), math.Round(zf)
	dx, dy, dz := math.Abs(rx-xf), math.Abs(ry-yf), math.Abs(rz-zf)
	switch {
	case dx > dy && dx > dz:
		rx = -ry - rz
	case dy > dz:
		ry = -rx - rz
	default:
		rz = -rx - ry
	}
	return Coord{Q: int(rx), R: int(rz)}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
