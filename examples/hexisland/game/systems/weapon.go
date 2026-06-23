package systems

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

const tracerMesh = "tracer"
const tracerMaterial = "tracer"

// TTL despawns its entity after T seconds — used for muzzle flashes and tracers.
type TTL struct{ T float32 }

// FireWeapon resolves the hitscan weapon each frame: it auto-fires while the
// mouse is held (and the cursor is captured), picks the nearest enemy in a
// forgiving cone with clear line of sight, applies damage, and spawns the juice
// — a muzzle-flash light, a tracer, view recoil, and a hit marker.
func FireWeapon(c *app.Ctx) {
	if !playing(c) {
		return
	}
	p := app.GetResource[Player](c)
	cb := app.GetResource[Combat](c)
	tm := app.GetResource[splititime.Time](c)
	if p == nil || cb == nil || tm == nil {
		return
	}
	dt := float32(tm.Delta().Seconds())
	cb.FireCD = maxF(0, cb.FireCD-dt)
	cb.MuzzleTime = maxF(0, cb.MuzzleTime-dt)
	cb.HitMarkerTime = maxF(0, cb.HitMarkerTime-dt)
	cb.HurtTime = maxF(0, cb.HurtTime-dt)

	// Only fire once focused, so the click that captures the cursor doesn't shoot.
	firing := p.Captured && render3d.MouseButtonDown(c, inputs.MouseButtonLeft)
	if !firing || cb.FireCD > 0 || cb.Ammo <= 0 {
		return
	}
	FireOnce(c, p, cb)
}

// FireOnce performs a single shot: spend ammo, set the cooldown and feedback
// timers, hitscan the nearest enemy in the aim cone with clear line of sight,
// apply damage, and spawn the muzzle flash and tracer. Split out from the input
// gate so tools and tests can drive a shot directly.
func FireOnce(c *app.Ctx, p *Player, cb *Combat) {
	cb.Ammo--
	cb.FireCD = WeaponFireInterval
	cb.MuzzleTime = MuzzleDuration
	p.recoil += RecoilKick
	playSound(c, "gunshot")

	eye := p.Eye()
	fwd := p.Forward()

	// Nearest enemy within range and aim cone. Aim at each enemy's body centre
	// (its transform plus AimHeight) so a nearby mob whose feet sit below the
	// crosshair still falls inside the cone.
	var bestEnt ecs.Entity
	var bestPos m.Vec3
	bestDist := float32(math.MaxFloat32)
	found := false
	app.Query2[Enemy, render3d.Transform3D](c, func(e ecs.Entity, en *Enemy, tr *render3d.Transform3D) {
		aimP := tr.Translation.Add(m.Vec3{Y: en.AimHeight})
		to := aimP.Sub(eye)
		dist := to.Length()
		if dist < 1e-4 || dist > ShotRange {
			return
		}
		if to.Scale(1/dist).Dot(fwd) < ShotConeCos {
			return
		}
		if dist < bestDist {
			bestDist, bestEnt, bestPos, found = dist, e, aimP, true
		}
	})

	end := eye.Add(fwd.Scale(ShotRange))
	switch {
	case found:
		// Confirm the shot isn't blocked by terrain closer than the target. The
		// target's own mesh (for a model enemy, a child of bestEnt rather than
		// bestEnt itself) must not count as a wall.
		hit, ok := render3d.Raycast(c, eye, bestPos.Sub(eye))
		blocked := ok && hit.Dist < bestDist-0.4 && !hitBelongsTo(c, hit.Entity, bestEnt)
		if blocked {
			end = hit.Point // a wall/hill is in the way — no damage
		} else {
			end = bestPos
			if damageEnemy(c, cb, bestEnt, ShotDamage) {
				playSound(c, "kill")
			} else {
				playSound(c, "hit")
			}
			cb.HitMarkerTime = HitMarkerDuration
		}
	default:
		// Missed everything: end the tracer on whatever terrain it strikes.
		if hit, ok := render3d.Raycast(c, eye, fwd); ok {
			end = hit.Point
		}
	}

	spawnTracer(c, eye, end)
	spawnMuzzleFlash(c, eye.Add(fwd.Scale(0.4)))
}

// hitBelongsTo reports whether a raycast hit entity is part of the target enemy
// — either the target itself (the primitive proxy) or a mesh in the target's
// model tree (an animated enemy, whose Enemy lives on the synthetic root while
// the meshes are child entities). Used so the line-of-sight test doesn't mistake
// the target's own body for a wall.
func hitBelongsTo(c *app.Ctx, hitEnt, target ecs.Entity) bool {
	if hitEnt == target {
		return true
	}
	mt := generic.NewMap[render3d.ModelTree](c.World())
	return mt.Has(hitEnt) && mt.Get(hitEnt).Root == target
}

// spawnTracer draws a brief glowing streak from the muzzle to the impact point.
func spawnTracer(c *app.Ctx, from, to m.Vec3) {
	seg := to.Sub(from)
	length := seg.Length()
	if length < 1e-3 {
		return
	}
	mid := from.Add(to).Scale(0.5)
	t := render3d.NewTransform3D(mid).Facing(seg).Scaled(0.03, 0.03, length)
	gt := render3d.GlobalTransform{Matrix: m.Identity4()}
	mr := render3d.MeshRenderer{Mesh: tracerMesh}
	ref := render3d.MaterialRef{Material: tracerMaterial}
	ttl := TTL{T: 0.05}
	mp := generic.NewMap5[
		render3d.Transform3D, render3d.GlobalTransform,
		render3d.MeshRenderer, render3d.MaterialRef, TTL,
	](c.World())
	mp.NewWith(&t, &gt, &mr, &ref, &ttl)
}

// spawnMuzzleFlash pops a short-lived warm point light at the muzzle.
func spawnMuzzleFlash(c *app.Ctx, pos m.Vec3) {
	t := render3d.NewTransform3D(pos)
	gt := render3d.GlobalTransform{Matrix: m.Identity4()}
	light := render3d.PointLight{Color: m.Vec3{X: 1, Y: 0.8, Z: 0.4}, Intensity: 6, Range: 6}
	ttl := TTL{T: MuzzleDuration}
	mp := generic.NewMap4[
		render3d.Transform3D, render3d.GlobalTransform, render3d.PointLight, TTL,
	](c.World())
	mp.NewWith(&t, &gt, &light, &ttl)
}

// ExpireTTL counts down transient effect entities and despawns them at zero.
func ExpireTTL(c *app.Ctx) {
	tm := app.GetResource[splititime.Time](c)
	if tm == nil {
		return
	}
	dt := float32(tm.Delta().Seconds())
	var dead []ecs.Entity
	app.Query1[TTL](c, func(e ecs.Entity, t *TTL) {
		t.T -= dt
		if t.T <= 0 {
			dead = append(dead, e)
		}
	})
	for _, e := range dead {
		c.Commands().Despawn(e)
	}
}
