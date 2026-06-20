package wfc

import (
	"math/rand"
	"testing"
)

func TestCompatibleGradient(t *testing.T) {
	if !Compatible(Water, Sand) || !Compatible(Grass, Grass) || !Compatible(Dirt, Stone) {
		t.Fatal("adjacent gradient terrains should be compatible")
	}
	if Compatible(Water, Grass) || Compatible(Sand, Dirt) || Compatible(Water, Stone) {
		t.Fatal("terrains more than one step apart must not be compatible")
	}
}

func TestNewBoardCount(t *testing.T) {
	b := NewBoard(5)
	// A radius-N hex board has 1 + 3N(N+1) cells.
	if got, want := len(b.Cells), 1+3*5*6; got != want {
		t.Fatalf("radius-5 board has %d cells, want %d", got, want)
	}
}

func TestWorldRoundTrip(t *testing.T) {
	b := NewBoard(5)
	for coord := range b.Cells {
		x, z := b.WorldXZ(coord)
		if got := FromWorld(x, z); got != coord {
			t.Fatalf("FromWorld(WorldXZ(%v)) = %v", coord, got)
		}
	}
}

func TestPropagationShrinksNeighbours(t *testing.T) {
	b := NewBoard(3)
	rng := rand.New(rand.NewSource(1))
	// Force a Water tile at the origin, then check a neighbour.
	origin := Coord{0, 0}
	b.Cells[origin].Domain = 1 << Water // only Water possible here
	if _, ok := b.Collapse(origin, rng); !ok {
		t.Fatal("collapse of origin failed")
	}
	// A neighbour of a Water cell may now only be Water or Sand.
	nb := b.Cells[Coord{1, 0}]
	want := uint8(1<<Water | 1<<Sand)
	if nb.Domain != want {
		t.Fatalf("neighbour domain = %05b, want %05b", nb.Domain, want)
	}
}

func TestFirstMoveAnywhereThenAdjacent(t *testing.T) {
	b := NewBoard(3)
	rng := rand.New(rand.NewSource(2))
	far := Coord{3, -3} // an in-bounds corner
	if !b.Placeable(far) {
		t.Fatal("first move should be allowed on any in-bounds cell")
	}
	b.Collapse(Coord{0, 0}, rng)
	if b.Placeable(far) {
		t.Fatal("after the first tile, far-away non-adjacent cells must not be placeable")
	}
	if !b.Placeable(Coord{1, 0}) {
		t.Fatal("a cell next to a placed tile must be placeable")
	}
}

func TestIslandOnlyInOpenWater(t *testing.T) {
	if !needsOpenWater["water-island"] {
		t.Fatal("water-island should be an open-water-only tile")
	}
	rng := rand.New(rand.NewSource(3))
	b := NewBoard(2)

	// A lone water tile next to empty cells is NOT surrounded by water yet, so
	// the centre must not be allowed to become an island.
	b.Cells[Coord{0, 0}].Domain = 1 << Water
	b.Collapse(Coord{0, 0}, rng)
	if b.allNeighboursWater(Coord{1, 0}) {
		t.Fatal("a cell with empty neighbours should not count as surrounded by water")
	}

	// Surround a centre cell with water; now it qualifies as open water.
	b2 := NewBoard(1) // centre + its 6 neighbours, nothing else
	for _, d := range axialDirs {
		c := Coord{d.Q, d.R}
		b2.Cells[c].Domain = 1 << Water
		if _, ok := b2.Collapse(c, rng); !ok {
			t.Fatalf("failed to place surrounding water at %v", c)
		}
	}
	if !b2.allNeighboursWater(Coord{0, 0}) {
		t.Fatal("a cell ringed by water tiles should count as open water")
	}
}

// neighbour is the coord across edge e from c.
func neighbour(c Coord, e int) Coord {
	return Coord{c.Q + axialDirs[e].Q, c.R + axialDirs[e].R}
}

// setTile collapses cell c to a named tile deterministically (no RNG), for
// setting up a known neighbourhood in a test.
func setTile(b *Board, c Coord, model string) {
	idx := -1
	for i, t := range Tiles {
		if t.Model == model {
			idx = i
			break
		}
	}
	if idx < 0 {
		panic("setTile: unknown model " + model)
	}
	cell := b.Cells[c]
	cell.Collapsed, cell.Tile, cell.Domain = true, idx, 1<<Tiles[idx].Terrain
	b.anyCollapsed = true
}

func TestHarbourNeedsShore(t *testing.T) {
	if !needsShore["building-dock"] || !needsShore["building-port"] {
		t.Fatal("dock and port should be shore tiles")
	}
	b := NewBoard(2)
	centre := Coord{0, 0}
	dock := TileDef{Model: "building-dock", Terrain: Sand}

	// Bare board: a harbour has neither a shore nor land for its green half.
	if b.allows(centre, dock) {
		t.Fatal("a harbour on a bare board must not be placeable")
	}
	// Land for the green half, but still no water → no shore.
	setTile(b, neighbour(centre, 2), "grass")
	if b.allows(centre, dock) {
		t.Fatal("a harbour with grass but no water must not be placeable")
	}
	// Water on the opposite edge → now it has both a shore and a green side.
	setTile(b, neighbour(centre, 5), "water")
	if !b.allows(centre, dock) {
		t.Fatal("a harbour with a grassy back and a water front should be placeable")
	}
}

// TestDockFacesGrass checks the orientation rule: a dock is spun so its grassy
// half faces an actual grass neighbour.
func TestDockFacesGrass(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	b := NewBoard(2)
	centre := Coord{0, 0}
	dock := TileDef{Model: "building-dock", Terrain: Sand}

	if b.canOrient(centre, "building-dock") {
		t.Fatal("a dock with no grass neighbour should not be orientable")
	}
	const grassEdge = 2
	setTile(b, neighbour(centre, grassEdge), "grass")
	setTile(b, neighbour(centre, (grassEdge+3)%6), "water")
	if !b.canOrient(centre, "building-dock") {
		t.Fatal("a dock with a grass neighbour should be orientable")
	}
	k := b.RotationStep(centre, dock, rng)
	if green := (orientedGreenEdge["building-dock"] + k) % 6; green != grassEdge {
		t.Fatalf("dock's green half faces edge %d, want the grass edge %d", green, grassEdge)
	}
}

func TestBuildingsStandApart(t *testing.T) {
	if !IsBuilding("building-castle") || IsBuilding("grass") {
		t.Fatal("IsBuilding should recognise only building-* tiles")
	}
	b := NewBoard(2)
	// Force a building at the centre by hand (any building index will do).
	bi := -1
	for i, td := range Tiles {
		if IsBuilding(td.Model) {
			bi = i
			break
		}
	}
	if bi < 0 {
		t.Fatal("no building tiles in the catalog")
	}
	centre := Coord{0, 0}
	cell := b.Cells[centre]
	cell.Domain, cell.Collapsed, cell.Tile = 1<<Grass, true, bi
	b.anyCollapsed = true

	nb := Coord{1, 0}
	house := TileDef{Model: "building-house", Terrain: Grass}
	if b.allows(nb, house) {
		t.Fatal("a building next to another building must be rejected")
	}
	if !b.allows(nb, TileDef{Model: "grass", Terrain: Grass}) {
		t.Fatal("plain grass next to a building should still be allowed")
	}
}

// TestTerrainClusters checks the neighbour-affinity bias: a cell whose
// neighbours are all water should almost always collapse to water too, rather
// than to the sand the bare gradient would allow just as readily.
func TestTerrainClusters(t *testing.T) {
	water := 0
	const trials = 200
	for seed := 0; seed < trials; seed++ {
		b := NewBoard(2)
		rng := rand.New(rand.NewSource(int64(seed)))
		// Ring the centre with water.
		for _, d := range axialDirs {
			c := Coord{d.Q, d.R}
			b.Cells[c].Domain = 1 << Water
			if _, ok := b.Collapse(c, rng); !ok {
				t.Fatalf("seed %d: failed to place surrounding water at %v", seed, c)
			}
		}
		tile, ok := b.Collapse(Coord{0, 0}, rng)
		if !ok {
			t.Fatalf("seed %d: centre failed to collapse", seed)
		}
		if tile.Terrain == Water {
			water++
		}
	}
	// Six matching neighbours make water clusterBias⁶ ≈ 244× the per-tile bias;
	// it should win nearly every time. (Uniform picking would be far lower.)
	if water < trials*9/10 {
		t.Fatalf("centre ringed by water became water only %d/%d times; clustering too weak", water, trials)
	}
}

// TestNeighboursAlwaysCompatible is the core guarantee: greedily collapsing
// placeable cells until none remain must never produce two adjacent tiles whose
// terrains violate the adjacency rules.
func TestNeighboursAlwaysCompatible(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 50; trial++ {
		b := NewBoard(5)
		for {
			var placeable []Coord
			for c := range b.Cells {
				if b.Placeable(c) {
					placeable = append(placeable, c)
				}
			}
			if len(placeable) == 0 {
				break
			}
			pick := placeable[rng.Intn(len(placeable))]
			if _, ok := b.Collapse(pick, rng); !ok {
				t.Fatalf("trial %d: collapse failed on a placeable cell %v", trial, pick)
			}
		}
		for coord, cell := range b.Cells {
			if !cell.Collapsed {
				continue
			}
			for _, d := range axialDirs {
				n := b.Cells[Coord{coord.Q + d.Q, coord.R + d.R}]
				if n == nil || !n.Collapsed {
					continue
				}
				if !Compatible(Tiles[cell.Tile].Terrain, Tiles[n.Tile].Terrain) {
					t.Fatalf("trial %d: incompatible neighbours %v(%d) / %v(%d)",
						trial, coord, Tiles[cell.Tile].Terrain, Coord{coord.Q + d.Q, coord.R + d.R}, Tiles[n.Tile].Terrain)
				}
			}
		}
	}
}
