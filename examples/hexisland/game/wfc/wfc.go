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
	"strings"
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
//
// Weight is the relative chance of this tile being picked among the legal
// choices for its terrain. Bare land is common, natural features less so, and
// the Kenney building tiles are rare landmarks — so a collapsed grass cell is
// usually plain grass, occasionally a forest or hill, and now and then a
// village, castle or windmill. A zero weight is treated as 1.
type TileDef struct {
	Model   string // base GLB filename without extension, e.g. "grass-forest"
	Terrain Terrain
	Weight  int
}

// weight returns the tile's selection weight, treating an unset (zero) weight
// as 1 so a tile is never impossible to pick.
func (t TileDef) weight() int {
	if t.Weight <= 0 {
		return 1
	}
	return t.Weight
}

// Selection-weight tiers. The spread is wide on purpose: bare terrain should
// dominate so islands read as wild, with the kit's structures scattered across
// them as occasional settlements rather than wall-to-wall city.
const (
	wBare    = 60 // plain terrain — the default face of a cell
	wFeature = 40 // a natural feature: forest, hill, rocks, dunes, mountain…
	wIsland  = 20 // lone island, already gated to open water (see openWaterOnly)
	wBuild   = 2  // a building landmark on grass
	wHarbor  = 3  // a coastal building on the sand rim
)

// Tiles is the catalog the game collapses cells to. All tiles of a terrain are
// interchangeable as far as the adjacency rules are concerned; the game picks
// among the matching ones at random, biased by Weight. The Kenney building
// tiles ride on the same hex bases as the bare terrain — most sit on grass, so
// they join the Grass pool; the dock and port carry a beach rim, so they join
// Sand and only ever appear where the coast allows.
var Tiles = []TileDef{
	// Water.
	{"water", Water, wBare}, {"water-rocks", Water, wFeature}, {"water-island", Water, wIsland},

	// Sand — bare dunes, plus the two coastal harbours.
	{"sand", Sand, wBare}, {"sand-desert", Sand, wFeature}, {"sand-rocks", Sand, wFeature},
	{"building-dock", Sand, wHarbor}, {"building-port", Sand, wHarbor},

	// Grass — bare meadow, natural features, then the village of buildings.
	{"grass", Grass, wBare}, {"grass-forest", Grass, wFeature}, {"grass-hill", Grass, wFeature},
	{"building-cabin", Grass, wBuild}, {"building-house", Grass, wBuild},
	{"building-village", Grass, wBuild}, {"building-farm", Grass, wBuild},
	{"building-sheep", Grass, wBuild}, {"building-market", Grass, wBuild},
	{"building-mill", Grass, wBuild}, {"building-watermill", Grass, wBuild},
	{"building-smelter", Grass, wBuild}, {"building-mine", Grass, wBuild},
	{"building-tower", Grass, wBuild}, {"building-wall", Grass, wBuild},
	{"building-walls", Grass, wBuild}, {"building-archery", Grass, wBuild},
	{"building-castle", Grass, wBuild}, {"building-wizard-tower", Grass, wBuild},

	// Dirt.
	{"dirt", Dirt, wBare}, {"dirt-lumber", Dirt, wFeature},

	// Stone.
	{"stone", Stone, wBare}, {"stone-hill", Stone, wFeature},
	{"stone-mountain", Stone, wFeature}, {"stone-rocks", Stone, wFeature},
}

const fullDomain = uint8(1<<terrainCount) - 1 // bitmask with every terrain possible

// Beyond the terrain gradient, a few tiles carry an extra placement rule about
// the tiles already collapsed around them. These are what keep the kit's
// special pieces believable: a lone island wants open sea, a harbour wants a
// shoreline to dock against, and the buildings spread out into separate
// settlements instead of fusing into one solid block. All three rules look only
// at already-collapsed neighbours, so they compose cleanly with the gradient and
// never need their own propagation pass.

// needsOpenWater tiles may sit only where every neighbour is water — a collapsed
// water tile, or the open ocean past the board edge. The gradient alone would
// let an island sit against a beach; this is what holds it out in the sea.
var needsOpenWater = map[string]bool{
	"water-island": true,
}

// needsShore tiles must touch the sea: at least one neighbour has to be water (a
// placed water tile, or open ocean off the board). They are the harbours — a
// dock or port stranded on dry land, with no water to reach, makes no sense.
var needsShore = map[string]bool{
	"building-dock": true,
	"building-port": true,
}

// orientedGreenEdge maps a directional tile to the axialDirs index its grassy
// half faces when spawned with no Y rotation. The dock and port are water/grass
// transition tiles: at rotation 0 the green half points at world +X, which is
// axialDirs[0]. Spinning the model +60° advances the facing one index along
// axialDirs, so at rotation step k the green half faces axialDirs[(edge+k)%6]
// and the water half the opposite edge. Such a tile may only be placed where its
// green half can face a grass tile, and it is turned to do so (see RotationStep).
var orientedGreenEdge = map[string]int{
	"building-dock": 0,
	"building-port": 0,
}

// IsBuilding reports whether a tile model is one of the kit's structures; every
// such model is named "building-…". Two buildings may not be neighbours, so each
// collapsed settlement reads as its own landmark rather than an unbroken city.
func IsBuilding(model string) bool {
	return strings.HasPrefix(model, "building-")
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
		if !b.allows(coord, t) {
			continue // fails an extra placement rule (open water / shore / spacing)
		}
		cands = append(cands, i)
	}
	if len(cands) == 0 {
		return TileDef{}, false
	}
	pick := b.weightedPick(coord, cands, rng)
	cell.Tile = pick
	cell.Collapsed = true
	cell.Domain = 1 << Tiles[pick].Terrain
	b.anyCollapsed = true
	b.propagate(coord)
	return Tiles[pick], true
}

// clusterBias is how strongly a cell prefers to match the terrain of its
// already-collapsed neighbours. Each neighbour that shares a candidate's terrain
// multiplies that candidate's weight by clusterBias, so a cell ringed by water
// becomes water with overwhelming odds (four matching neighbours make its
// terrain clusterBias⁴ ≈ 39× likelier), three matching neighbours strongly
// favour it, and a lone neighbour only nudges. This is what turns the legal-but-
// arbitrary gradient picks into coherent regions — open sea, broad beaches,
// forests and mountain ranges — instead of a salt-and-pepper scatter. It is a
// soft bias layered on top of the hard gradient: it only ever re-weights choices
// the rules already allow, so it can never produce an illegal neighbour.
const clusterBias = 2.5

// weightedPick chooses a tile index from cands, biased by two things: each
// tile's base Weight (bare terrain common, buildings rare) and how well its
// terrain matches coord's already-collapsed neighbours (terrain coherence — see
// clusterBias). Candidates of the same terrain share the same neighbour bonus,
// so the bias steers which *terrain* a cell takes while the base weights still
// decide which variant of it. cands must be non-empty.
func (b *Board) weightedPick(coord Coord, cands []int, rng *rand.Rand) int {
	counts := b.neighbourTerrainCounts(coord)
	weights := make([]float64, len(cands))
	var total float64
	for j, i := range cands {
		w := float64(Tiles[i].weight()) * math.Pow(clusterBias, float64(counts[Tiles[i].Terrain]))
		weights[j] = w
		total += w
	}
	roll := rng.Float64() * total
	for j, i := range cands {
		roll -= weights[j]
		if roll < 0 {
			return i
		}
	}
	return cands[len(cands)-1] // unreachable: weights sum to total
}

// neighbourTerrainCounts tallies, per terrain, how many of coord's six
// neighbours have already settled on that terrain. Open ocean past the board
// edge counts as water, so coastlines naturally pull more sea around themselves
// and islands keep a watery border.
func (b *Board) neighbourTerrainCounts(coord Coord) [terrainCount]int {
	var counts [terrainCount]int
	for _, d := range axialDirs {
		cell := b.Cells[Coord{coord.Q + d.Q, coord.R + d.R}]
		switch {
		case cell == nil:
			counts[Water]++ // open ocean off the board
		case cell.Collapsed:
			counts[Tiles[cell.Tile].Terrain]++
		}
	}
	return counts
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

// allows reports whether tile t may be collapsed at coord given the extra
// placement rules layered on top of the terrain gradient. The gradient itself is
// already enforced by the cell's domain, so this only checks the per-tile rules,
// and only ever inspects already-collapsed neighbours. Bare terrain carries no
// rule, so for any terrain still in the domain at least its plain tile always
// passes — the candidate set can never be emptied by these rules.
func (b *Board) allows(coord Coord, t TileDef) bool {
	if needsOpenWater[t.Model] && !b.allNeighboursWater(coord) {
		return false
	}
	if needsShore[t.Model] && !b.touchesWater(coord) {
		return false
	}
	if !b.canOrient(coord, t.Model) {
		return false // a dock/port with no grass neighbour for its green half to face
	}
	if IsBuilding(t.Model) && b.touchesBuilding(coord) {
		return false
	}
	return true
}

// neighbourAt returns the terrain across edge e (axialDirs[e]) and whether that
// neighbour is decided: a collapsed tile gives its own terrain, open ocean off
// the board counts as Water, and an empty in-bounds cell is undecided (ok=false).
func (b *Board) neighbourAt(coord Coord, e int) (Terrain, bool) {
	d := axialDirs[e]
	cell := b.Cells[Coord{coord.Q + d.Q, coord.R + d.R}]
	switch {
	case cell == nil:
		return Water, true // open ocean past the edge
	case cell.Collapsed:
		return Tiles[cell.Tile].Terrain, true
	default:
		return 0, false
	}
}

// canOrient reports whether a directional tile may be placed at coord: it must
// have at least one rotation whose grassy half faces an actual grass tile, so
// "the dock's green part connects to a green tile" can be honoured. Tiles that
// are not directional are unconstrained here.
func (b *Board) canOrient(coord Coord, model string) bool {
	edge, oriented := orientedGreenEdge[model]
	if !oriented {
		return true
	}
	for k := 0; k < 6; k++ {
		if t, ok := b.neighbourAt(coord, (edge+k)%6); ok && t == Grass {
			return true
		}
	}
	return false
}

// RotationStep returns the spawn rotation (0..5, ×60°) for tile t at coord. A
// directional tile (dock/port) is turned so its grassy half faces a grass
// neighbour — and, among the rotations that achieve that, so its water half
// points at the sea where possible. Every other tile gets a random spin for
// variety. It assumes a directional tile was only placed where canOrient held,
// so a valid facing always exists.
func (b *Board) RotationStep(coord Coord, t TileDef, rng *rand.Rand) int {
	edge, oriented := orientedGreenEdge[t.Model]
	if !oriented {
		return rng.Intn(6)
	}
	best, bestScore := 0, -1
	for k := 0; k < 6; k++ {
		if gt, ok := b.neighbourAt(coord, (edge+k)%6); !ok || gt != Grass {
			continue // green half must face grass
		}
		score := 0 // prefer the water half to face open water, then a beach
		if wt, ok := b.neighbourAt(coord, (edge+k+3)%6); ok {
			switch wt {
			case Water:
				score = 2
			case Sand:
				score = 1
			}
		}
		if score > bestScore {
			best, bestScore = k, score
		}
	}
	return best
}

// touchesWater reports whether any neighbour of coord is water — a collapsed
// water tile, or off the board (open ocean). It's the shoreline test a harbour
// needs: somewhere adjacent to dock against.
func (b *Board) touchesWater(coord Coord) bool {
	for _, d := range axialDirs {
		cell := b.Cells[Coord{coord.Q + d.Q, coord.R + d.R}]
		if cell == nil {
			return true // open ocean past the edge is sea enough to dock against
		}
		if cell.Collapsed && Tiles[cell.Tile].Terrain == Water {
			return true
		}
	}
	return false
}

// touchesBuilding reports whether any neighbour of coord has already collapsed to
// a building, used to keep settlements from fusing into one solid block.
func (b *Board) touchesBuilding(coord Coord) bool {
	for _, d := range axialDirs {
		cell := b.Cells[Coord{coord.Q + d.Q, coord.R + d.R}]
		if cell != nil && cell.Collapsed && IsBuilding(Tiles[cell.Tile].Model) {
			return true
		}
	}
	return false
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
