// Package systems holds the gameplay systems for the first-person shooter: the
// auto world generation, the FPS player controller, and (in the combat files)
// the enemies, weapon, and HUD. The wave-function rules that shape the island
// live in the engine-free game/wfc package; these systems are the bridge
// between that model and render3d.
package systems

import (
	"math/rand"
	"time"

	"github.com/hvuhsg/spliti/plugin/render3d"

	"hexisland/game/wfc"
)

// BoardRadius is the size of the hexagonal island (radius in hex steps).
const BoardRadius = 5

// Game is the world-state resource: the wfc board the island is generated from
// and the loaded tile models a collapse spawns. Player and combat state live in
// their own resources (see player.go, combat.go).
type Game struct {
	Board  *wfc.Board
	Models map[string]*render3d.Model // tile model name -> loaded glTF model

	// Heights is the walkable surface height per hex, sampled once from the
	// spawned tiles (see EnsureHeights). The player and enemies stand on it
	// instead of raycasting every frame, which also stops a downward ray from
	// catching a mob's own body.
	Heights map[wfc.Coord]float32

	Rng *rand.Rand
}

// NewGame builds the initial world state: an empty board (filled in at startup
// by GenerateWorld) and a fresh RNG. Models is filled by LoadAssets.
func NewGame() *Game {
	return &Game{
		Board:  wfc.NewBoard(BoardRadius),
		Models: map[string]*render3d.Model{},
		Rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}
