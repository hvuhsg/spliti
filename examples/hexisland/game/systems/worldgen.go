package systems

import (
	"sort"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/generic"

	"hexisland/game/wfc"
)

// IslandRadius is the world-space radius of the containment dome and the hard
// player clamp. The outermost tile centres sit ~BoardRadius units from the
// origin (unit hexes), so +1 leaves a little breathing room past the last tile.
const IslandRadius float32 = BoardRadius + 1

const (
	barrierMesh     = "barrier"
	barrierMaterial = "barrier"
)

// GenerateWorld collapses the entire wfc board at startup — a one-shot solver
// that floods outward from the centre — and spawns every chosen tile model.
// Where the old game let the player click cells one at a time, the island now
// builds itself before the player ever takes control.
func GenerateWorld(c *app.Ctx) {
	g := app.GetResource[Game](c)
	if g == nil {
		return
	}

	// Seed the island core so Placeable starts requiring adjacency (the board
	// grows outward from here rather than scattering).
	seed := wfc.Coord{Q: 0, R: 0}
	if tile, ok := g.Board.Collapse(seed, g.Rng); ok {
		spawnTile(c, g, seed, tile)
	}

	// Flood-fill: each pass collapses every cell that is currently placeable.
	// Placeable requires an already-collapsed neighbour, so the frontier expands
	// by one ring per pass and the loop terminates once the board is full (≤ the
	// cell count, with a hard cap as a belt-and-braces guard against a stuck cell).
	for guard := 0; guard < len(g.Board.Cells)+1; guard++ {
		var frontier []wfc.Coord
		for coord := range g.Board.Cells {
			if g.Board.Placeable(coord) {
				frontier = append(frontier, coord)
			}
		}
		if len(frontier) == 0 {
			break
		}
		// Map iteration is random; sort so a given RNG seed yields a repeatable
		// island (useful for the headless agent checks).
		sort.Slice(frontier, func(i, j int) bool {
			if frontier[i].Q != frontier[j].Q {
				return frontier[i].Q < frontier[j].Q
			}
			return frontier[i].R < frontier[j].R
		})
		for _, coord := range frontier {
			// A cell can stop being placeable mid-pass once a neighbour collapses,
			// but Collapse re-checks, so a failed collapse just skips it.
			if tile, ok := g.Board.Collapse(coord, g.Rng); ok {
				spawnTile(c, g, coord, tile)
			}
		}
	}
}

// spawnTile instantiates one collapsed cell's tile model at its world position,
// turned the way the wfc rules want it (random spin for most tiles, oriented for
// docks/ports). Lifted from the old click-to-place interaction.
func spawnTile(c *app.Ctx, g *Game, coord wfc.Coord, tile wfc.TileDef) {
	model := g.Models[tile.Model]
	if model == nil {
		return
	}
	x, z := g.Board.WorldXZ(coord)
	rotDeg := float32(60 * g.Board.RotationStep(coord, tile, g.Rng))
	render3d.SpawnModel(c.Commands(), model,
		render3d.XForm().At(x, 0, z).EulerDeg(0, rotDeg, 0))
}

// SpawnBarrier creates the translucent containment dome — a glassy sphere
// centred on the island. It is purely visual feedback; the authoritative
// containment is the position clamp in PlayerMove (see clampToIsland). The dome
// is tagged Transparent so it draws in the back-to-front blended pass.
func SpawnBarrier(c *app.Ctx) {
	mp := generic.NewMap5[
		render3d.Transform3D, render3d.GlobalTransform,
		render3d.MeshRenderer, render3d.MaterialRef, render3d.Transparent,
	](c.World())
	t := render3d.NewTransform3D(m.Vec3{})
	gt := render3d.GlobalTransform{Matrix: m.Identity4()}
	mr := render3d.MeshRenderer{Mesh: barrierMesh}
	ref := render3d.MaterialRef{Material: barrierMaterial}
	tr := render3d.Transparent{}
	mp.NewWith(&t, &gt, &mr, &ref, &tr)
}

// EnsureHeights samples the walkable surface height of every hex once, by
// casting a ray straight down through each cell centre onto the spawned tiles.
// It is idempotent and cheap to call from the per-frame systems; it only does
// work the first time, once the tile entities exist (they are spawned through
// Commands during startup, so the first physics tick is the earliest the rays
// can hit them).
func EnsureHeights(c *app.Ctx, g *Game) {
	if g.Heights != nil {
		return
	}
	// The barrier dome is spawned only after this map is built (see the commit
	// below), so these rays see only the tiles — a missed tile yields no hit
	// rather than silently striking the dome far below.
	found := make(map[wfc.Coord]float32, len(g.Board.Cells))
	var sum float32
	for coord := range g.Board.Cells {
		x, z := g.Board.WorldXZ(coord)
		origin := m.Vec3{X: x, Y: 2.5, Z: z} // above tiles, below where the dome would be
		if hit, ok := render3d.Raycast(c, origin, m.Vec3{Y: -1}); ok {
			found[coord] = hit.Point.Y
			sum += hit.Point.Y
		}
	}
	// On the first tick the tiles' GlobalTransforms aren't propagated yet (still
	// identity, stacking every tile at the origin), so almost nothing is hit.
	// Wait until most cells resolve, then commit on a later tick.
	if len(found) < len(g.Board.Cells)/2 {
		return
	}
	avg := sum / float32(len(found))
	heights := make(map[wfc.Coord]float32, len(g.Board.Cells))
	for coord := range g.Board.Cells {
		if h, ok := found[coord]; ok {
			heights[coord] = h
		} else {
			heights[coord] = avg // a tile the ray slipped past: use the island average
		}
	}
	g.Heights = heights
	SpawnBarrier(c) // now safe to raise the dome; the ground rays are done
}

// GroundHeightAt returns the walkable height of the hex under a world (x,z), and
// whether that hex has a sampled surface (false off the island).
func GroundHeightAt(g *Game, x, z float32) (float32, bool) {
	if g.Heights == nil {
		return 0, false
	}
	h, ok := g.Heights[wfc.FromWorld(x, z)]
	return h, ok
}
