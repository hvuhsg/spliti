// Package systems holds the gameplay systems: the orbit camera, the
// hover/place interaction, and the per-cell marker coloring. The wave-function
// rules themselves live in the engine-free game/wfc package; these systems are
// the thin bridge between that model and render3d.
package systems

import (
	"math/rand"
	"time"

	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"

	"hexisland/game/wfc"
)

// BoardRadius is the size of the hexagonal play area (radius in hex steps).
const BoardRadius = 5

// Game is the single game-state resource. It owns the wfc board, the loaded
// tile models (so a collapse can spawn the right one), the orbit-camera angles,
// and the current hover/click state.
type Game struct {
	Board  *wfc.Board
	Models map[string]*render3d.Model // tile model name -> loaded glTF model

	// Orbit camera: angles + distance around a movable focus point (Center),
	// which the player can pan across the board.
	Yaw, Pitch, Dist float32
	Center           m.Vec3

	// Mouse-drag camera state, tracked across frames by CameraControl.
	orbiting, panning  bool
	lastCurX, lastCurY float64

	// Hover/placement state, refreshed every frame by Interact.
	Hover     wfc.Coord
	HasHover  bool
	prevClick bool

	Rng *rand.Rand
}

// NewGame builds the initial game state: an empty board and a camera looking
// down at it from a comfortable angle. Models is filled by LoadAssets.
func NewGame() *Game {
	return &Game{
		Board:  wfc.NewBoard(BoardRadius),
		Models: map[string]*render3d.Model{},
		Yaw:    0.6,
		Pitch:  0.95,
		Dist:   20,
		Rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}
