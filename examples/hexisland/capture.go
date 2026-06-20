//go:build !js

package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/plugin/screenshot"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"

	"hexisland/game/systems"
	"hexisland/game/wfc"
)

// hexCap drives the optional self-screenshot session.
type hexCap struct {
	placed  bool
	elapsed float32
	dwell   float32
	shot    bool
	poseCam bool // tile/scene modes pose the camera manually each frame
	target  m.Vec3
	dist    float64
	yaw     float64
	pitch   float64
}

// addCapture wires a one-shot screenshot driver, selected by env vars and
// writing its PNG to SPLITI_HEX_SHOT. Three modes:
//
//   - SPLITI_HEX_TILE=<name>   render one tile, framed close (for the rules docs)
//   - SPLITI_HEX_SCENE=<name>  render a fixed example layout (allowed/forbidden)
//   - default                  auto-grow a random island (gameplay screenshot)
//
// Inert without SPLITI_HEX_SHOT, so a normal `go run` is unaffected.
func addCapture(a *app.App) {
	out := os.Getenv("SPLITI_HEX_SHOT")
	if out == "" {
		return
	}
	dwell := float32(0.5)
	if s, err := strconv.ParseFloat(os.Getenv("SPLITI_HEX_SHOT_SECONDS"), 32); err == nil && s > 0 {
		dwell = float32(s)
	}
	tileName := os.Getenv("SPLITI_HEX_TILE")
	sceneName := os.Getenv("SPLITI_HEX_SCENE")

	st := &hexCap{dwell: dwell}
	app.InsertResource(a, st)
	a.AddSystems(schedule.Update, app.System(func(c *app.Ctx) {
		if st.shot {
			c.App().Stop()
			return
		}
		if !st.placed {
			switch {
			case tileName != "":
				setupTile(c, st, tileName)
			case sceneName != "":
				setupScene(c, st, sceneName)
			default:
				growIsland(c, islandTiles())
				applyCamOverride(c)
			}
			st.placed = true
			return // let the queued spawns apply before dwelling
		}
		// In tile/scene modes keep posing the camera, since CameraControl runs
		// every frame and would otherwise clamp/override our close framing.
		if st.poseCam {
			if cam := app.GetResource[render3d.Camera3D](c); cam != nil {
				poseCamera(cam, st.target, st.dist, st.yaw, st.pitch)
			}
		}
		if tm := app.GetResource[splititime.Time](c); tm != nil {
			st.elapsed += float32(tm.Delta().Seconds())
		}
		if st.elapsed < st.dwell {
			return
		}
		if screenshot.Save(c, out) {
			st.shot = true
		}
	}).Label("hex-capture"))
}

func islandTiles() int {
	if n, err := strconv.Atoi(os.Getenv("SPLITI_HEX_SHOT_TILES")); err == nil && n > 0 {
		return n
	}
	return 42
}

// placement is one tile in a fixed example scene.
type placement struct {
	model string
	q, r  int
}

// scenes are the fixed layouts the rules docs illustrate. The neighbour order
// is the six axial directions around a centre tile.
var exampleScenes = map[string][]placement{
	// A straight run across the whole gradient — every adjacency here is legal.
	"gradient": {{"water", -2, 0}, {"sand", -1, 0}, {"grass", 0, 0}, {"dirt", 1, 0}, {"stone", 2, 0}},

	// An island ringed entirely by water (the open-water-only rule in action).
	"island": {{"water-island", 0, 0},
		{"water", 1, 0}, {"water-rocks", 1, -1}, {"water", 0, -1},
		{"water", -1, 0}, {"water-rocks", -1, 1}, {"water", 0, 1}},

	// A tile surrounded by examples of its legal neighbours.
	"around-water": {{"water", 0, 0},
		{"water-rocks", 1, 0}, {"sand", 1, -1}, {"water", 0, -1},
		{"sand-rocks", -1, 0}, {"water", -1, 1}, {"sand-desert", 0, 1}},
	"around-sand": {{"sand", 0, 0},
		{"water", 1, 0}, {"grass", 1, -1}, {"sand-rocks", 0, -1},
		{"water-rocks", -1, 0}, {"grass-forest", -1, 1}, {"sand-desert", 0, 1}},
	"around-grass": {{"grass", 0, 0},
		{"sand", 1, 0}, {"grass-forest", 1, -1}, {"grass-hill", 0, -1},
		{"dirt", -1, 0}, {"dirt-lumber", -1, 1}, {"sand-rocks", 0, 1}},
	"around-dirt": {{"dirt", 0, 0},
		{"grass", 1, 0}, {"dirt-lumber", 1, -1}, {"stone", 0, -1},
		{"stone-hill", -1, 0}, {"grass-hill", -1, 1}, {"stone-rocks", 0, 1}},
	"around-stone": {{"stone", 0, 0},
		{"dirt", 1, 0}, {"stone-hill", 1, -1}, {"stone-mountain", 0, -1},
		{"stone-rocks", -1, 0}, {"dirt-lumber", -1, 1}, {"stone", 0, 1}},

	// A harbour on the shore, oriented the way the rules place it: its green half
	// (which at rotation 0 faces +X, the {1,0} edge) backs onto grass, and its
	// water half faces the open sea on the far side.
	"harbour": {{"building-port", 0, 0},
		{"grass", 1, 0}, {"grass-forest", 1, -1}, {"sand", 0, 1}, // land behind the green half
		{"water", 0, -1}, {"water", -1, 0}, {"water-rocks", -1, 1}}, // sea before the water half

	// Two buildings kept apart by the spacing rule: a castle and a mill on
	// separate grass cells with plain ground between them.
	"settlements": {{"building-castle", -1, 0}, {"grass", 0, 0}, {"building-mill", 1, 0},
		{"grass-forest", 0, -1}, {"grass-hill", 0, 1}},

	// Forbidden adjacencies, force-placed for contrast (the rules would reject
	// these — they exist only so the docs can show what NOT to allow).
	"forbidden-mountain-sea":       {{"water", 0, 0}, {"stone-mountain", 1, 0}},
	"forbidden-island-beach":       {{"water-island", 0, 0}, {"sand", 1, 0}},
	"forbidden-harbour-inland":     {{"building-dock", 0, 0}, {"sand", 1, 0}, {"grass", 1, -1}, {"sand", 0, -1}, {"sand-rocks", -1, 0}, {"grass", -1, 1}, {"sand-desert", 0, 1}},
	"forbidden-buildings-adjacent": {{"building-house", 0, 0}, {"building-market", 1, 0}},
}

// setupTile renders a single tile at the origin, framed close.
func setupTile(c *app.Ctx, st *hexCap, name string) {
	g := app.GetResource[systems.Game](c)
	if g == nil {
		return
	}
	hideAllMarkers(g)
	spawnTile(c, g, placement{name, 0, 0})
	st.poseCam = true
	st.target = m.Vec3{Y: 0.12}
	st.dist, st.yaw, st.pitch = 2.6, 0.7, 0.82
	// SPLITI_HEX_TOPDOWN frames the tile from straight overhead (world +X to the
	// right, +Z down), used to read a tile's edge layout — e.g. which hex edge a
	// dock's grassy half faces.
	if os.Getenv("SPLITI_HEX_TOPDOWN") != "" {
		st.target = m.Vec3{}
		st.dist, st.yaw, st.pitch = 2.4, 0, 1.5605
	}
}

// setupScene renders one of the named example layouts and frames it.
func setupScene(c *app.Ctx, st *hexCap, name string) {
	g := app.GetResource[systems.Game](c)
	if g == nil {
		return
	}
	tiles, ok := exampleScenes[name]
	if !ok {
		fmt.Printf("hex-capture: unknown scene %q\n", name)
		return
	}
	hideAllMarkers(g)
	var sumX, sumZ float32
	var maxR float64
	for _, p := range tiles {
		spawnTile(c, g, p)
		x, z := g.Board.WorldXZ(wfc.Coord{Q: p.q, R: p.r})
		sumX, sumZ = sumX+x, sumZ+z
	}
	cx, cz := sumX/float32(len(tiles)), sumZ/float32(len(tiles))
	for _, p := range tiles {
		x, z := g.Board.WorldXZ(wfc.Coord{Q: p.q, R: p.r})
		if d := math.Hypot(float64(x-cx), float64(z-cz)); d > maxR {
			maxR = d
		}
	}
	st.poseCam = true
	st.target = m.Vec3{X: cx, Y: 0.12, Z: cz}
	st.dist = 2.6 + 2.4*maxR
	st.yaw, st.pitch = 0.7, 0.9
}

// spawnTile places one tile model at its axial coordinate (no rotation, so the
// docs are reproducible). It bypasses the WFC board — these are illustrations.
func spawnTile(c *app.Ctx, g *systems.Game, p placement) {
	model := g.Models[p.model]
	if model == nil {
		fmt.Printf("hex-capture: no model %q\n", p.model)
		return
	}
	x, z := g.Board.WorldXZ(wfc.Coord{Q: p.q, R: p.r})
	var rotDeg float32
	if s := os.Getenv("SPLITI_HEX_TILE_ROT"); s != "" {
		if k, err := strconv.Atoi(s); err == nil {
			rotDeg = float32(60 * k)
		}
	}
	render3d.SpawnModel(c.Commands(), model, render3d.XForm().At(x, 0, z).EulerDeg(0, rotDeg, 0))
}

// hideAllMarkers marks every board cell collapsed so UpdateMarkers hides all the
// buildable-cell glows, leaving a clean backdrop for the rules images.
func hideAllMarkers(g *systems.Game) {
	for _, cell := range g.Board.Cells {
		cell.Collapsed = true
	}
}

// poseCamera aims the camera at target from a yaw/pitch/distance orbit, without
// the gameplay clamps in CameraControl (so we can frame tiles up close).
func poseCamera(cam *render3d.Camera3D, target m.Vec3, dist, yaw, pitch float64) {
	cp, sp := math.Cos(pitch), math.Sin(pitch)
	sy, cy := math.Sin(yaw), math.Cos(yaw)
	cam.Position = m.Vec3{
		X: target.X + float32(dist*cp*sy),
		Y: target.Y + float32(dist*sp),
		Z: target.Z + float32(dist*cp*cy),
	}
	cam.Target = target
	cam.Up = m.Vec3{Y: 1}
}

// applyCamOverride lets a screenshot pose the island camera via
// SPLITI_HEX_CAM="yaw,pitch,dist,centerX,centerZ".
func applyCamOverride(c *app.Ctx) {
	spec := os.Getenv("SPLITI_HEX_CAM")
	if spec == "" {
		return
	}
	g := app.GetResource[systems.Game](c)
	if g == nil {
		return
	}
	var yaw, pitch, dist, cxp, czp float64
	if n, _ := fmt.Sscanf(spec, "%f,%f,%f,%f,%f", &yaw, &pitch, &dist, &cxp, &czp); n >= 3 {
		g.Yaw, g.Pitch, g.Dist = float32(yaw), float32(pitch), float32(dist)
		g.Center.X, g.Center.Z = float32(cxp), float32(czp)
	}
}

// growIsland collapses up to n placeable cells at random — exactly what the
// player's clicks do — and spawns each chosen tile, so the screenshot shows a
// real wave-function-collapsed island with the buildable-cell markers around it.
func growIsland(c *app.Ctx, n int) {
	g := app.GetResource[systems.Game](c)
	if g == nil {
		return
	}
	// SPLITI_HEX_SEED reseeds the board RNG so a screenshot is reproducible (and
	// a batch run gets a distinct island per seed). Without it the game's normal
	// time-based seed gives a fresh island each launch.
	if s := os.Getenv("SPLITI_HEX_SEED"); s != "" {
		if seed, err := strconv.ParseInt(s, 10, 64); err == nil {
			g.Rng = rand.New(rand.NewSource(seed))
		}
	}
	placed := 0
	for placed < n {
		var ps []wfc.Coord
		for coord := range g.Board.Cells {
			if g.Board.Placeable(coord) {
				ps = append(ps, coord)
			}
		}
		if len(ps) == 0 {
			break
		}
		pick := ps[g.Rng.Intn(len(ps))]
		tile, ok := g.Board.Collapse(pick, g.Rng)
		if !ok {
			break
		}
		if model := g.Models[tile.Model]; model != nil {
			x, z := g.Board.WorldXZ(pick)
			rot := float32(60 * g.Board.RotationStep(pick, tile, g.Rng))
			render3d.SpawnModel(c.Commands(), model,
				render3d.XForm().At(x, 0, z).EulerDeg(0, rot, 0))
		}
		placed++
	}
	fmt.Printf("hex-capture: grew island with %d tiles\n", placed)
}
