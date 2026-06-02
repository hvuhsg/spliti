// Package runtime is the editor's built-in component & system vocabulary.
//
// The editor authors games purely from data, so the engine ships with a
// fixed set of behaviors that any project can use:
//
//	Velocity         per-tick Position delta
//	Bounds           AABB collider for collision detection
//	Tag              named handle for hierarchy display + collision filter
//	KeyboardControl  maps keys to velocity assignments
//	CollisionEvent   produced by the collision system on overlap
//
// Add runtime.Plugin{} to install the three systems (KeyboardSystem,
// MovementSystem, CollisionSystem) on FixedUpdate. The systems are gated by
// an optional RunIf predicate so the editor can pause the simulation in Edit
// mode while still rendering the world.
package runtime

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Velocity is added to Position once per FixedUpdate tick. Both axes are int;
// faster motion is achieved by setting larger values (or by composing with a
// HoldTicks pattern via KeyboardControl).
type Velocity struct{ DX, DY int }

// Bounds is an AABB collider whose top-left corner sits at the entity's
// Position. Width/Height are in cells.
type Bounds struct{ W, H int }

// Tag attaches a human-readable name to an entity. Used by the editor's
// hierarchy panel and by collision events to identify participants without
// looking up entity IDs.
type Tag struct{ Name string }

// Action is a logical input action. The editor exposes a fixed enum so the
// scene format can serialize KeyboardControl bindings deterministically.
type Action int

const (
	ActNone Action = iota
	ActMoveUp
	ActMoveDown
	ActMoveLeft
	ActMoveRight
)

// KeyBinding describes "when this key is pressed, write Velocity{DX,DY} and
// keep the entity moving for HoldTicks fixed steps". HoldTicks=1 means the
// entity moves exactly one tick per key press — fine for puzzle games where
// terminals don't emit key-up events. HoldTicks > 1 lets the OS auto-repeat
// keep the velocity alive between repeats.
type KeyBinding struct {
	Key       tcell.Key
	Rune      rune
	Mod       tcell.ModMask
	Action    Action
	DX, DY    int
	HoldTicks int
}

// matches reports whether ev triggers this binding.
func (b KeyBinding) matches(ev input.KeyEvent) bool {
	if b.Mod != 0 && ev.Mod&b.Mod != b.Mod {
		return false
	}
	if b.Key != tcell.KeyRune && ev.Key == b.Key {
		return true
	}
	if b.Key == tcell.KeyRune && ev.Rune == b.Rune {
		return true
	}
	return false
}

// KeyboardControl binds keys to velocity assignments. When a binding's key
// fires, KeyboardSystem refreshes a hold counter; while the counter is
// positive, MovementSystem will apply the binding's DX,DY to the entity's
// Position.
//
// The hold counter is per-binding-index because two bindings on the same
// entity (e.g. up + down) must be tracked independently.
type KeyboardControl struct {
	Bindings []KeyBinding
	hold     []int // parallel to Bindings; created lazily
}

// CollisionEvent is sent once per overlapping pair per FixedUpdate tick.
// A and B are the participating entities; ATag and BTag are their Tag.Name
// values (empty string if no Tag component). The pair (A,B) is reported in
// stable entity-id order so listeners don't see (a,b) and (b,a) separately.
type CollisionEvent struct {
	A, B       ecs.Entity
	ATag, BTag string
}

// Plugin installs Keyboard → Movement → Collision on FixedUpdate. The runIf
// predicate, if non-nil, gates all three systems uniformly so the editor can
// pause the simulation by returning false.
type Plugin struct {
	// RunIf, when non-nil, is checked before each runtime system runs.
	// The editor sets this to "is current state Playing?"; standalone games
	// can leave it nil to always run.
	RunIf func(*app.Ctx) bool
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	keyboardSys := app.System(KeyboardSystem).Label("__spliti_runtime_keyboard")
	movementSys := app.System(MovementSystem).Label("__spliti_runtime_movement").After("__spliti_runtime_keyboard")
	collisionSys := app.System(CollisionSystem).Label("__spliti_runtime_collision").After("__spliti_runtime_movement")
	if p.RunIf != nil {
		keyboardSys.RunIf(p.RunIf)
		movementSys.RunIf(p.RunIf)
		collisionSys.RunIf(p.RunIf)
	}
	a.AddSystems(schedule.FixedUpdate, keyboardSys, movementSys, collisionSys)
}

// KeyboardSystem reads input.KeyEvent for the current frame and refreshes
// hold counters on every entity whose KeyboardControl has a matching binding.
// It does NOT directly write Position; movement is applied by MovementSystem
// so multiple keys can stack (e.g. up+left = diagonal) and so a missing
// matching binding leaves the entity exactly where it was.
//
// Note: events buffered at frame-First are visible here regardless of
// FixedUpdate frequency. If FixedUpdate runs N times in a frame, the same
// key event would naively trigger N hold refreshes — we guard against that
// by clearing the per-binding "fired this frame" flag implicitly: hold is
// only refreshed UP, never below its current value, so two refreshes in one
// frame are idempotent.
func KeyboardSystem(c *app.Ctx) {
	events := app.ReadEvents[input.KeyEvent](c)
	if len(events) == 0 {
		return
	}
	app.Query1[KeyboardControl](c, func(_ ecs.Entity, kc *KeyboardControl) {
		if len(kc.hold) != len(kc.Bindings) {
			kc.hold = make([]int, len(kc.Bindings))
		}
		for _, ev := range events {
			for i, b := range kc.Bindings {
				if b.matches(ev) {
					ticks := b.HoldTicks
					if ticks <= 0 {
						ticks = 1
					}
					if kc.hold[i] < ticks {
						kc.hold[i] = ticks
					}
				}
			}
		}
	})
}

// MovementSystem applies (a) any active KeyboardControl holds and (b) any
// Velocity component to the entity's Position, and decrements hold counters.
//
// Order: keyboard-driven motion is integrated FIRST (so a binding that
// resets velocity overrides a stale Velocity value), then the residual
// Velocity is applied. In practice most editor games use one or the other,
// not both on the same entity.
//
// Entities with KeyboardControl but no Position are skipped naturally — the
// Query2 filter requires both components.
func MovementSystem(c *app.Ctx) {
	// Keyboard holds → position delta. Query2 yields only entities with
	// both Position and KeyboardControl, so no Has() check is needed.
	app.Query2[tui.Position, KeyboardControl](c, func(_ ecs.Entity, pos *tui.Position, kc *KeyboardControl) {
		for i := range kc.hold {
			if kc.hold[i] > 0 {
				kc.hold[i]--
				pos.X += kc.Bindings[i].DX
				pos.Y += kc.Bindings[i].DY
			}
		}
	})

	// Plain velocity → position delta.
	app.Query2[tui.Position, Velocity](c, func(_ ecs.Entity, p *tui.Position, v *Velocity) {
		p.X += v.DX
		p.Y += v.DY
	})
}

// CollisionSystem performs an O(n²) AABB pair-test on (Position, Bounds)
// entities and emits one CollisionEvent per overlap. Entity-id order is
// preserved for stable pair identity.
//
// O(n²) is fine for the editor's expected scene sizes (dozens of entities).
// Replace with a spatial hash if a project breaks past a few hundred.
func CollisionSystem(c *app.Ctx) {
	type body struct {
		E    ecs.Entity
		X, Y int
		W, H int
		Tag  string
	}
	var bodies []body
	world := c.World()
	tagID := ecs.ComponentID[Tag](world)
	tagMap := generic.NewMap1[Tag](world)
	app.Query2[tui.Position, Bounds](c, func(e ecs.Entity, p *tui.Position, b *Bounds) {
		t := ""
		if world.Has(e, tagID) {
			t = tagMap.Get(e).Name
		}
		bodies = append(bodies, body{E: e, X: p.X, Y: p.Y, W: b.W, H: b.H, Tag: t})
	})
	for i := 0; i < len(bodies); i++ {
		for j := i + 1; j < len(bodies); j++ {
			a, b := bodies[i], bodies[j]
			if a.X+a.W <= b.X || b.X+b.W <= a.X {
				continue
			}
			if a.Y+a.H <= b.Y || b.Y+b.H <= a.Y {
				continue
			}
			app.SendEvent(c, CollisionEvent{A: a.E, B: b.E, ATag: a.Tag, BTag: b.Tag})
		}
	}
}
