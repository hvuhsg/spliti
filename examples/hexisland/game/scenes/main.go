// Package scenes holds the game's scene setup functions. The spliti editor
// reads and edits these files — keep spawn arguments literal.
package scenes

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"

	"hexisland/game/entities"
	"hexisland/game/systems"
)

//spliti:scene Main
func Main(c *app.Ctx) {
	_ = scene.Spawn(c, "sun", entities.SpawnSun(c, render3d.XForm().EulerDeg(-55, 35, 0)))
	// Build the empty hex board: one glowing marker per cell. Tiles themselves
	// are spawned at play time as the player collapses cells.
	systems.SpawnBoard(c)
}
