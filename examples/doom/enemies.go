package main

import (
	"fmt"
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// enemyAI advances each enemy one tick: if it can see the player it walks
// toward them (sliding along walls), and when in melee range it bites on a
// cooldown.
func enemyAI(c *app.Ctx) {
	p := app.GetResource[Player](c)
	g := app.GetResource[Game](c)

	app.Query2[Enemy, Vec2](c, func(_ ecs.Entity, en *Enemy, v *Vec2) {
		if en.Cooldown > 0 {
			en.Cooldown--
		}

		dx, dy := p.X-v.X, p.Y-v.Y
		dist := math.Hypot(dx, dy)

		if dist <= meleeRange {
			if en.Cooldown == 0 {
				p.HP -= enemyDamage
				p.HurtTicks = hurtTicks
				en.Cooldown = enemyAttackCD
			}
			return
		}

		// Only chase when there's a clear line of sight.
		if !losClear(g.Map, v.X, v.Y, p.X, p.Y) || dist < 1e-6 {
			return
		}
		ux, uy := dx/dist, dy/dist
		v.X, v.Y = moveWithCollision(g.Map, v.X, v.Y, ux*enemySpeed, uy*enemySpeed)
	})
}

// resolveFiring consumes a queued shot. A hitscan picks the nearest enemy that
// lies within a narrow cone around the look direction, inside range, with no
// wall in between, and damages it.
func resolveFiring(c *app.Ctx) {
	p := app.GetResource[Player](c)
	fire := p.FireQueued
	p.FireQueued = false
	if !fire || p.FireCD > 0 {
		return
	}
	if p.Ammo <= 0 {
		setMessage(c, "*click* out of ammo")
		return
	}

	p.Ammo--
	p.FireCD = fireCooldown
	p.MuzzleTicks = muzzleTicks

	g := app.GetResource[Game](c)

	var (
		bestEnt   ecs.Entity
		bestDist  = math.MaxFloat64
		bestFound bool
	)
	app.Query2[Enemy, Vec2](c, func(e ecs.Entity, _ *Enemy, v *Vec2) {
		dx, dy := v.X-p.X, v.Y-p.Y
		dist := math.Hypot(dx, dy)
		if dist < 1e-6 || dist > shotRange {
			return
		}
		// Angle between look direction and the enemy.
		dot := (dx*p.DirX + dy*p.DirY) / dist
		if dot < shotConeCos {
			return
		}
		if !losClear(g.Map, p.X, p.Y, v.X, v.Y) {
			return
		}
		if dist < bestDist {
			bestDist, bestEnt, bestFound = dist, e, true
		}
	})

	if !bestFound {
		return
	}
	emap := generic.NewMap1[Enemy](c.World())
	en := emap.Get(bestEnt)
	en.HP -= shotDamage
	if en.HP <= 0 {
		c.Commands().Despawn(bestEnt)
		p.Kills++
	}
}

// collectPickups grants nearby pickups to the player and despawns them.
func collectPickups(c *app.Ctx) {
	p := app.GetResource[Player](c)
	app.Query2[Pickup, Vec2](c, func(e ecs.Entity, pk *Pickup, v *Vec2) {
		if math.Hypot(p.X-v.X, p.Y-v.Y) > pickupRange {
			return
		}
		switch pk.Kind {
		case pickHealth:
			p.HP += pk.Amount
			if p.HP > playerMaxHP {
				p.HP = playerMaxHP
			}
			setMessage(c, fmt.Sprintf("+%d health", pk.Amount))
		case pickAmmo:
			p.Ammo += pk.Amount
			setMessage(c, fmt.Sprintf("+%d ammo", pk.Amount))
		}
		c.Commands().Despawn(e)
	})
}

// checkEndConditions flips the state machine to Dead or Won.
func checkEndConditions(c *app.Ctx) {
	p := app.GetResource[Player](c)
	if p.HP <= 0 {
		p.HP = 0
		app.GetState[GameMode](c).Set(Dead)
		return
	}
	remaining := 0
	app.Query1[Enemy](c, func(ecs.Entity, *Enemy) { remaining++ })
	if remaining == 0 {
		app.GetState[GameMode](c).Set(Won)
	}
}

func setMessage(c *app.Ctx, msg string) {
	g := app.GetResource[Game](c)
	g.Message = msg
	g.MsgTicks = msgTicks
}

// losClear reports whether the straight segment from (x0,y0) to (x1,y1) crosses
// no solid cell — a cheap stepped ray march, accurate enough for AI sight and
// hitscan over the level's short distances.
func losClear(m GameMap, x0, y0, x1, y1 float64) bool {
	dx, dy := x1-x0, y1-y0
	dist := math.Hypot(dx, dy)
	if dist < 1e-6 {
		return true
	}
	steps := int(dist / 0.05)
	if steps < 1 {
		steps = 1
	}
	sx, sy := dx/float64(steps), dy/float64(steps)
	x, y := x0, y0
	for i := 0; i < steps; i++ {
		x += sx
		y += sy
		if m.solid(int(x), int(y)) {
			return false
		}
	}
	return true
}
