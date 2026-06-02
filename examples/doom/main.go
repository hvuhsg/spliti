// Command doom is a first-person raycaster — a small "Doom-lite" — built on the
// spliti engine. It demonstrates that the engine can host a real-time 3D-style
// renderer: walls are cast with the classic DDA algorithm into a truecolor
// half-block framebuffer (see plugin/canvas), enemies and pickups are drawn as
// depth-buffered billboards, and the whole scene is pushed to the terminal as a
// single tui overlay each frame.
//
// Controls:
//
//	W / Up      move forward          A / Left   turn left
//	S / Down    move back             D / Right  turn right
//	Z           strafe left           E          strafe right
//	Space       fire                  R          restart (when dead / won)
//	Esc / Q     quit
//
// Run it with:
//
//	go run ./examples/doom
//
// A truecolor terminal gives the best results; tcell down-samples colors
// elsewhere.
package main

import (
	gotime "time"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/canvas"
	"github.com/hvuhsg/spliti/plugin/defaultplugins"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
)

// --- Tunables ------------------------------------------------------------

const (
	moveSpeed   = 0.075 // world units per fixed tick
	strafeSpeed = 0.065
	turnSpeed   = 0.045 // radians per fixed tick
	holdTicks   = 6     // ticks a key press keeps acting (smooths key-repeat)

	enemySpeed    = 0.035
	meleeRange    = 1.3
	enemyDamage   = 7
	enemyAttackCD = 45 // ticks between an enemy's melee hits

	fireCooldown = 7    // ticks between shots
	shotDamage   = 34   // imp has 100 HP -> 3 shots
	shotRange    = 22.0 // max hitscan distance
	shotConeCos  = 0.985

	pickupRange = 0.7
	playerMaxHP = 100
	startAmmo   = 40

	muzzleTicks = 4  // muzzle-flash duration
	hurtTicks   = 5  // red screen-flash duration when hit
	msgTicks    = 90 // pickup message duration
)

// Enemy/pickup kind ids.
const (
	enemyImp = 0

	pickHealth = 0
	pickAmmo   = 1
)

// --- Components -----------------------------------------------------------

// Vec2 is a floating-point world position. Mobs use this instead of tui's
// integer Position because the raycaster needs sub-cell precision.
type Vec2 struct{ X, Y float64 }

// Enemy marks a hostile mob.
type Enemy struct {
	Kind     uint8
	HP       int
	Cooldown int // ticks until it can melee again
}

// Pickup marks a collectible.
type Pickup struct {
	Kind   uint8
	Amount int
}

// --- Resources ------------------------------------------------------------

// Player holds the camera and combat state. Direction and camera plane are
// stored as vectors (Lode Vandevenne's formulation) so rotation is a matrix
// multiply and the FOV is encoded in the plane length.
type Player struct {
	X, Y           float64
	DirX, DirY     float64
	PlaneX, PlaneY float64
	HP             int
	Ammo           int
	Kills          int

	FireQueued  bool
	FireCD      int
	MuzzleTicks int
	HurtTicks   int
}

// Hold tracks how many more ticks each movement intent stays active. Terminals
// deliver key-repeat as discrete events with an initial delay, so we latch each
// press for a few ticks to make motion feel continuous (same trick as
// examples/survivors).
type Hold struct {
	Fwd, Back        int
	TurnL, TurnR     int
	StrafeL, StrafeR int
}

// Game holds the level and per-frame render scratch buffers.
type Game struct {
	Map    GameMap
	Canvas *canvas.Canvas
	Depth  []float64 // perpendicular wall distance per screen column

	Message  string
	MsgTicks int
}

// GameMode is the top-level state machine value.
type GameMode int

const (
	Playing GameMode = iota
	Dead
	Won
)

func main() {
	a := app.New()
	a.AddPlugins(defaultplugins.Plugins{
		Time: splititime.Plugin{
			FixedTimestep:   16 * gotime.Millisecond,
			TargetFrameRate: 60,
		},
	})

	app.InsertResource(a, &Player{})
	app.InsertResource(a, &Hold{})
	app.InsertResource(a, &Game{})
	app.InitState(a, Playing)

	// setupLevel runs via OnEnter(Playing); the engine fires it for the
	// initial state at startup, and again whenever the player restarts. We do
	// NOT also register it on Startup or the level would be built twice.
	app.OnEnter(a, Playing, setupLevel)

	a.AddSystems(schedule.Update, handleInput)
	a.AddSystems(schedule.FixedUpdate, step)
	tui.AddOverlay(a, render)

	a.Run()
}

// step is the fixed-timestep simulation. It only advances while Playing; the
// renderer keeps drawing in every state so the death/win banners stay visible.
func step(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	g := app.GetResource[Game](c)
	if g.MsgTicks > 0 {
		g.MsgTicks--
	}
	movePlayer(c)
	enemyAI(c)
	resolveFiring(c)
	collectPickups(c)
	checkEndConditions(c)
}
