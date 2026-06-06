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
// Add runtime.Plugin{} to install keyboard, movement, and collision on
// FixedUpdate. Collision is the reusable plugin/collision package (Bounds and
// CollisionEvent are aliases for its Collider and CollisionEvent); runtime just
// wires it with a Tag resolver. The systems are gated by an optional RunIf
// predicate so the editor can pause the simulation in Edit mode while still
// rendering the world.
package runtime

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/collision"
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
//
// It is an alias for collision.Collider: the collision detection itself lives in
// the reusable plugin/collision package, and runtime just keeps the familiar
// name (and the editor's "bounds" component) pointing at it. The aliased type
// also carries optional Layer/Mask filter fields, which scenes that don't set
// them leave zero — meaning "collide with everything", exactly as before.
type Bounds = collision.Collider

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
// stable iteration order so listeners don't see (a,b) and (b,a) separately.
//
// It is an alias for collision.CollisionEvent, so listeners can read the event
// from either package interchangeably.
type CollisionEvent = collision.CollisionEvent

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
	// Collision detection is the reusable plugin/collision package; runtime
	// wires it with a Tag resolver so events carry the entity's Tag.Name, and
	// keeps the existing label/ordering so the editor's pause gate and tests
	// are unaffected.
	collisionSys := app.System(collision.NewSystem(collision.Config{Tag: resolveTag})).
		Label("__spliti_runtime_collision").After("__spliti_runtime_movement")
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

// resolveTag returns the entity's Tag.Name, or "" if it has no Tag component.
// It is handed to collision.NewSystem so CollisionEvent participants keep their
// human-readable labels without the collision package depending on runtime.Tag.
func resolveTag(world *ecs.World, e ecs.Entity) string {
	tagID := ecs.ComponentID[Tag](world)
	if world.Has(e, tagID) {
		tagMap := generic.NewMap1[Tag](world)
		return tagMap.Get(e).Name
	}
	return ""
}
