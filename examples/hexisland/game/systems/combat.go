package systems

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/mlange-42/arche/ecs"

	"hexisland/game/wfc"
)

// GameMode is the top-level state machine: an endless-wave run, then death.
type GameMode int

const (
	Playing GameMode = iota
	Dead
)

// Combat tuning.
const (
	MagSize            = 30
	WeaponFireInterval = 0.10  // seconds between shots (auto-fire)
	ShotDamage         = 34.0  // enemy base HP is 100 -> 3 shots
	ShotRange          = 45.0  // max hitscan distance
	ShotConeCos        = 0.985 // ~10° aim cone (forgiving crosshair)
	MuzzleDuration     = 0.05
	HitMarkerDuration  = 0.12
	HurtDuration       = 0.35
	RecoilKick         = 0.022 // radians of upward kick per shot
	RecoilRecover      = 9.0   // recoil decay per second

	EnemyBaseHP     = 100.0
	EnemyBaseSpeed  = 2.3
	EnemyMeleeRange = 1.5
	EnemyDamage     = 9.0
	EnemyAttackCD   = 0.9 // seconds between an enemy's hits

	WaveBreak    = 3.0 // seconds between waves
	SpawnStagger = 0.5 // seconds between spawns within a wave
	PlayerMaxHP  = 100.0
)

// Combat is the run state: ammo, wave/score progress, and the transient
// feedback timers the HUD and camera read. Player health lives on Player (the
// controller owns the body); everything combat-only lives here.
type Combat struct {
	Ammo int

	FireCD        float32
	MuzzleTime    float32
	HitMarkerTime float32
	HurtTime      float32

	Wave         int
	Kills        int
	EnemiesAlive int
	pendingSpawn int     // enemies still to spawn this wave
	spawnTimer   float32 // until the next spawn / next wave
	betweenWaves bool

	Monsters map[string]*render3d.Model // kind -> animated model (nil => primitive proxy)

	spawnCells []wfc.Coord // candidate edge tiles enemies spawn on
}

// NewCombat builds the combat resource; StartRun fills the run-specific fields.
func NewCombat() *Combat {
	return &Combat{Monsters: map[string]*render3d.Model{}}
}

// playing reports whether the run is live (combat sim runs only then).
func playing(c *app.Ctx) bool { return app.GetState[GameMode](c).Get() == Playing }

// StartRun (OnEnter Playing) resets the run: it clears any leftover enemies and
// effects, refills the player, and arms the first wave. It fires once at startup
// for the initial state and again on every restart, so it must be idempotent and
// must NOT regenerate the world (the island persists across restarts).
func StartRun(c *app.Ctx) {
	cb := app.GetResource[Combat](c)
	p := app.GetResource[Player](c)
	g := app.GetResource[Game](c)
	if cb == nil || p == nil || g == nil {
		return
	}

	despawnAllEnemies(c)
	despawnAllTTL(c)

	cb.Ammo = MagSize
	cb.Wave = 0
	cb.Kills = 0
	cb.EnemiesAlive = 0
	cb.pendingSpawn = 0
	cb.spawnTimer = 1.0 // brief grace before the first wave
	cb.betweenWaves = true
	cb.FireCD = 0
	cb.MuzzleTime = 0
	cb.HitMarkerTime = 0
	cb.HurtTime = 0

	cb.spawnCells = spawnCandidates(g)
	SpawnPlayer(c) // repositions on a walkable central cell and refills health
}

// SpawnWaves advances the wave clock: it spaces enemy spawns within a wave and,
// once a wave is cleared, waits WaveBreak before arming a larger next wave.
func SpawnWaves(c *app.Ctx) {
	if !playing(c) {
		return
	}
	cb := app.GetResource[Combat](c)
	g := app.GetResource[Game](c)
	tm := app.GetResource[splititime.Time](c)
	if cb == nil || g == nil || tm == nil {
		return
	}
	dt := float32(tm.Delta().Seconds())
	cb.spawnTimer -= dt

	if cb.betweenWaves {
		if cb.spawnTimer <= 0 {
			cb.Wave++
			cb.pendingSpawn = enemiesForWave(cb.Wave)
			cb.Ammo = MagSize // resupply at the start of each wave
			cb.betweenWaves = false
			cb.spawnTimer = 0
		}
		return
	}

	if cb.pendingSpawn > 0 && cb.spawnTimer <= 0 {
		SpawnEnemy(c, g, cb)
		cb.pendingSpawn--
		cb.EnemiesAlive++
		cb.spawnTimer = SpawnStagger
		return
	}

	// Wave cleared: take a breather, then arm a bigger wave.
	if cb.pendingSpawn == 0 && cb.EnemiesAlive <= 0 {
		cb.betweenWaves = true
		cb.spawnTimer = WaveBreak
	}
}

// enemiesForWave scales the wave size: 3, 5, 7, …
func enemiesForWave(wave int) int { return 1 + 2*wave }

// CheckEndConditions ends the run when the player dies.
func CheckEndConditions(c *app.Ctx) {
	if !playing(c) {
		return
	}
	p := app.GetResource[Player](c)
	if p != nil && p.Health <= 0 {
		p.Health = 0
		if p.Captured {
			render3d.SetMouseCaptured(c, false)
			p.Captured = false
		}
		app.GetState[GameMode](c).Set(Dead)
	}
}

// HandleRestart restarts a finished run when the player presses R.
func HandleRestart(c *app.Ctx) {
	if playing(c) {
		return
	}
	if render3d.KeyDown(c, inputs.KeyR) {
		app.GetState[GameMode](c).Set(Playing)
	}
}

// spawnCandidates collects the outer-ring, non-water land tiles enemies spawn on
// (so they come from the island's edges), falling back to any land tile.
func spawnCandidates(g *Game) []wfc.Coord {
	var edge, land []wfc.Coord
	for coord, cell := range g.Board.Cells {
		if cell == nil || !cell.Collapsed || wfc.Tiles[cell.Tile].Terrain == wfc.Water {
			continue
		}
		land = append(land, coord)
		if hexDist(coord) >= BoardRadius-1 {
			edge = append(edge, coord)
		}
	}
	if len(edge) > 0 {
		return edge
	}
	return land
}

// hexDist is the ring distance of an axial coordinate from the centre.
func hexDist(coord wfc.Coord) int {
	q, r := coord.Q, coord.R
	return (absInt(q) + absInt(r) + absInt(q+r)) / 2
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// despawnAllEnemies removes every enemy (and its model tree) from the world.
func despawnAllEnemies(c *app.Ctx) {
	type kill struct {
		e       ecs.Entity
		isModel bool
	}
	var kills []kill
	app.Query1[Enemy](c, func(e ecs.Entity, en *Enemy) {
		kills = append(kills, kill{e, en.IsModel})
	})
	for _, k := range kills {
		if k.isModel {
			render3d.DespawnModelTree(c, k.e)
		} else {
			c.Commands().Despawn(k.e)
		}
	}
}

// despawnAllTTL clears transient effect entities (muzzle flashes, tracers).
func despawnAllTTL(c *app.Ctx) {
	var ents []ecs.Entity
	app.Query1[TTL](c, func(e ecs.Entity, _ *TTL) { ents = append(ents, e) })
	for _, e := range ents {
		c.Commands().Despawn(e)
	}
}
