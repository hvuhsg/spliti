package systems

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"

	"hexisland/game/components"
	"hexisland/game/wfc"
)

const markerMesh = "marker"
const markerMaterial = "marker"

// SpawnBoard creates one flat hex marker entity per board cell. The markers are
// translucent and tinted per-frame by UpdateMarkers via their InstanceColor, so
// they can fade in as buildable spots and brighten under the cursor without any
// spawning/despawning during play. Called once from the Main scene.
func SpawnBoard(c *app.Ctx) {
	g := app.GetResource[Game](c)
	if g == nil {
		return
	}
	mp := generic.NewMap7[
		render3d.Transform3D, render3d.GlobalTransform,
		render3d.MeshRenderer, render3d.MaterialRef,
		render3d.InstanceColor, render3d.Transparent,
		components.HexCell,
	](c.World())

	for coord := range g.Board.Cells {
		x, z := g.Board.WorldXZ(coord)
		t := render3d.NewTransform3D(m.Vec3{X: x, Y: markerPlaneY, Z: z})
		gt := render3d.GlobalTransform{Matrix: m.Identity4()}
		mr := render3d.MeshRenderer{Mesh: markerMesh}
		ref := render3d.MaterialRef{Material: markerMaterial}
		col := hidden
		tr := render3d.Transparent{}
		hc := components.HexCell{Q: coord.Q, R: coord.R}
		mp.NewWith(&t, &gt, &mr, &ref, &col, &tr, &hc)
	}
}

// Marker tints: alpha 0 hides a marker entirely (the tile model, if any, shows
// through); buildable cells glow faintly; the hovered cell glows brightly.
var (
	hidden    = render3d.InstanceColor{R: 0.4, G: 0.7, B: 1, A: 0}
	buildable = render3d.InstanceColor{R: 0.45, G: 0.85, B: 1, A: 0.35}
	hovered   = render3d.InstanceColor{R: 1, G: 0.95, B: 0.4, A: 0.9}
)

// UpdateMarkers recolors every marker to reflect the current board: collapsed
// and non-buildable cells are hidden, buildable cells glow faintly, and the
// hovered cell glows bright and lifts slightly off the board.
func UpdateMarkers(c *app.Ctx) {
	g := app.GetResource[Game](c)
	if g == nil {
		return
	}
	app.Query3[components.HexCell, render3d.InstanceColor, render3d.Transform3D](c,
		func(_ ecs.Entity, hc *components.HexCell, col *render3d.InstanceColor, tr *render3d.Transform3D) {
			coord := wfc.Coord{Q: hc.Q, R: hc.R}
			x, z := g.Board.WorldXZ(coord)
			y := markerPlaneY
			cell := g.Board.Cells[coord]
			switch {
			case cell != nil && cell.Collapsed:
				*col = hidden
			case g.HasHover && coord == g.Hover:
				*col = hovered
				y = markerPlaneY + 0.12 // lift the hovered tile footprint
			case g.Board.Placeable(coord):
				*col = buildable
			default:
				*col = hidden
			}
			tr.Translation = m.Vec3{X: x, Y: y, Z: z}
		})
}
