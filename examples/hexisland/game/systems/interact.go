package systems

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"

	"hexisland/game/wfc"
)

// markerPlaneY is the height of the flat hex markers; the cursor ray is
// intersected with this plane to decide which cell is under the mouse.
const markerPlaneY float32 = 0.2

// Interact turns the mouse into gameplay: every frame it works out which board
// cell the cursor is over (and whether it is buildable), and on a left click it
// collapses that cell — the wave-function rules pick the tile — and spawns the
// chosen model on the board.
func Interact(c *app.Ctx) {
	g := app.GetResource[Game](c)
	if g == nil {
		return
	}

	// Hover: unproject the cursor, hit the marker plane, snap to a hex.
	g.HasHover = false
	px, py := render3d.CursorPos(c)
	origin, dir := render3d.ScreenToRay(c, px, py)
	if dir.Y != 0 {
		t := (markerPlaneY - origin.Y) / dir.Y
		if t > 0 {
			hx := origin.X + dir.X*t
			hz := origin.Z + dir.Z*t
			coord := wfc.FromWorld(hx, hz)
			if g.Board.Placeable(coord) {
				g.Hover = coord
				g.HasHover = true
			}
		}
	}

	// Place: on the click's leading edge, collapse the hovered cell and spawn
	// whatever tile the rules chose. Most tiles get a random 60° spin for variety;
	// a directional tile (a dock/port) is turned so its green half faces grass.
	click := render3d.MouseButtonDown(c, inputs.MouseButtonLeft)
	if click && !g.prevClick && g.HasHover {
		if tile, ok := g.Board.Collapse(g.Hover, g.Rng); ok {
			if model := g.Models[tile.Model]; model != nil {
				x, z := g.Board.WorldXZ(g.Hover)
				rotDeg := float32(60 * g.Board.RotationStep(g.Hover, tile, g.Rng))
				render3d.SpawnModel(c.Commands(), model,
					render3d.XForm().At(x, 0, z).EulerDeg(0, rotDeg, 0))
			}
			g.HasHover = false // the cell is now filled
		}
	}
	g.prevClick = click
}
