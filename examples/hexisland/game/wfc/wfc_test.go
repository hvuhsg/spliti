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
	if !openWaterOnly["water-island"] {
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
