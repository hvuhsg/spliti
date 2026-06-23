package systems

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// EnemyBodyRadius sizes the primitive monster proxy and lifts its centre off the
// ground so it sits on the surface.
const EnemyBodyRadius = 0.45

const (
	enemyMesh     = "enemy"
	enemyMaterial = "enemy"
)

// foxScale shrinks the (centimetre-scale) Fox model to roughly 1.3 units tall.
const foxScale = 0.011

// Enemy is a hostile mob. The entity it is attached to carries the Transform3D
// that is the mob's position. For the animated model that entity is the model's
// synthetic root (so moving it moves the whole rig); for the primitive proxy it
// is the renderable sphere itself. YOffset lifts the transform off the sampled
// ground so the body sits on the surface.
type Enemy struct {
	HP, MaxHP float32
	Speed     float32
	AttackCD  float32
	Kind      uint8
	IsModel   bool
	YOffset   float32 // transform height above the sampled ground
	AimHeight float32 // body-centre height above the transform, for hitscan aim
}

// SpawnEnemy creates one monster on a random edge tile. It uses the loaded
// animated creature when available, falling back to a glowing sphere proxy so
// the game is playable without the art.
func SpawnEnemy(c *app.Ctx, g *Game, cb *Combat) {
	if len(cb.spawnCells) == 0 {
		return
	}
	coord := cb.spawnCells[g.Rng.Intn(len(cb.spawnCells))]
	x, z := g.Board.WorldXZ(coord)
	groundY, _ := GroundHeightAt(g, x, z)

	en := Enemy{
		HP:    EnemyBaseHP,
		MaxHP: EnemyBaseHP,
		Speed: EnemyBaseSpeed + 0.15*float32(cb.Wave-1), // ramp speed with waves
		Kind:  0,
	}

	if model := cb.Monsters["fox"]; model != nil {
		en.IsModel = true
		en.YOffset = 0     // the model's origin is at its feet
		en.AimHeight = 0.5 // aim at the body, well above the feet
		t := render3d.XForm().At(x, groundY, z).Scaled(foxScale, foxScale, foxScale)
		root := render3d.NewModel(c, model, t)
		emap := generic.NewMap1[Enemy](c.World())
		emap.Assign(root, &en)
		// Play the run cycle (clip 2) instead of the idle survey (clip 0). The Fox
		// has animations, so NewModel always attaches an Animator to the root.
		amap := generic.NewMap1[render3d.Animator](c.World())
		amap.Get(root).Clip = 2
		return
	}

	// Primitive proxy fallback.
	en.YOffset = EnemyBodyRadius
	en.AimHeight = 0 // the sphere centre is already at the transform
	t := render3d.NewTransform3D(m.Vec3{X: x, Y: groundY + EnemyBodyRadius, Z: z})
	gt := render3d.GlobalTransform{Matrix: m.Identity4()}
	mr := render3d.MeshRenderer{Mesh: enemyMesh}
	ref := render3d.MaterialRef{Material: enemyMaterial}
	mp := generic.NewMap5[
		render3d.Transform3D, render3d.GlobalTransform,
		render3d.MeshRenderer, render3d.MaterialRef, Enemy,
	](c.World())
	mp.NewWith(&t, &gt, &mr, &ref, &en)
}

// EnemyAI advances every mob a fixed step: chase the player across the ground,
// hug the surface height, face the player, and bite on a cooldown when close.
func EnemyAI(c *app.Ctx) {
	if !playing(c) {
		return
	}
	p := app.GetResource[Player](c)
	g := app.GetResource[Game](c)
	cb := app.GetResource[Combat](c)
	tm := app.GetResource[splititime.Time](c)
	if p == nil || g == nil || cb == nil || tm == nil {
		return
	}
	EnsureHeights(c, g)
	dt := float32(tm.FixedDelta().Seconds())

	app.Query2[Enemy, render3d.Transform3D](c, func(_ ecs.Entity, en *Enemy, tr *render3d.Transform3D) {
		if en.AttackCD > 0 {
			en.AttackCD = maxF(0, en.AttackCD-dt)
		}

		dx := p.Pos.X - tr.Translation.X
		dz := p.Pos.Z - tr.Translation.Z
		dist := float32(math.Hypot(float64(dx), float64(dz)))

		if dist <= EnemyMeleeRange {
			if en.AttackCD <= 0 {
				p.Health -= EnemyDamage
				cb.HurtTime = HurtDuration
				en.AttackCD = EnemyAttackCD
				playSound(c, "hurt")
			}
		} else if dist > 1e-4 {
			ux, uz := dx/dist, dz/dist
			tr.Translation.X += ux * en.Speed * dt
			tr.Translation.Z += uz * en.Speed * dt
		}

		// Stick to the surface and face the player.
		if gy, ok := GroundHeightAt(g, tr.Translation.X, tr.Translation.Z); ok {
			tr.Translation.Y = gy + en.YOffset
		}
		if dist > 1e-4 {
			// The Fox model's forward is +Z, but Facing aligns -Z to the given
			// direction, so face the way the mob is moving by pointing -Z away
			// from the player (i.e. +Z toward it).
			*tr = tr.Facing(m.Vec3{X: -dx, Z: -dz})
		}
	})
}

// damageEnemy applies hitscan damage to a mob, despawning it (and its model) and
// scoring a kill when it dies. Returns whether the mob died.
func damageEnemy(c *app.Ctx, cb *Combat, ent ecs.Entity, dmg float32) bool {
	if !c.World().Alive(ent) {
		return false
	}
	emap := generic.NewMap1[Enemy](c.World())
	en := emap.Get(ent)
	en.HP -= dmg
	if en.HP > 0 {
		return false
	}
	if en.IsModel {
		render3d.DespawnModelTree(c, ent)
	} else {
		c.Commands().Despawn(ent)
	}
	cb.Kills++
	cb.EnemiesAlive--
	return true
}
