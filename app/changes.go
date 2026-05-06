package app

import (
	"fmt"
	"reflect"

	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/ecs/event"
	"github.com/mlange-42/arche/generic"
	"github.com/mlange-42/arche/listener"
)

// changeBuffer records entities whose component of type T was added or
// explicitly marked dirty since the last frame drain. One per tracked type.
type changeBuffer struct {
	entities []ecs.Entity
	seen     map[ecs.Entity]struct{}
}

func (b *changeBuffer) add(e ecs.Entity) {
	if _, ok := b.seen[e]; ok {
		return
	}
	b.seen[e] = struct{}{}
	b.entities = append(b.entities, e)
}

func (b *changeBuffer) drain() {
	if len(b.entities) == 0 {
		return
	}
	b.entities = b.entities[:0]
	for k := range b.seen {
		delete(b.seen, k)
	}
}

// TrackChanges enables change tracking for component T. Call during plugin
// Build (or any time before the first call to QueryChanged[T]) — once
// registered, every subsequent ComponentAdded for T (and every MarkChanged
// call) appends the entity to the per-frame change buffer, which is drained
// after the Last stage.
//
// Tracking detects two kinds of changes:
//
//   - Additions: an entity gained component T. Detected automatically via
//     arche's Listener on the world.
//   - Mutations: a system wrote through a *T pointer. Go can't intercept
//     pointer writes, so the writer must call MarkChanged[T] explicitly.
//
// Re-registering the same type is a no-op.
func TrackChanges[T any](a *App) {
	key := typeKey[T]()
	if _, ok := a.changeBuffers[key]; ok {
		return
	}
	buf := &changeBuffer{seen: make(map[ecs.Entity]struct{})}
	a.changeBuffers[key] = buf

	id := ecs.ComponentID[T](&a.world)
	cb := listener.NewCallback(func(_ *ecs.World, e ecs.EntityEvent) {
		buf.add(e.Entity)
	}, event.ComponentAdded, id)
	// Keep the Callback alive for the life of the App; AddListener stores by
	// ecs.Listener (interface) and Callback's methods have pointer receivers.
	a.changeListeners = append(a.changeListeners, &cb)
	a.dispatch.AddListener(&cb)
}

// QueryChanged1 iterates entities whose component T was added or marked
// dirty since the last frame drain. The pointer is live; mutating it does not
// re-mark the entity (the caller is already in the changed set).
//
// Panics if TrackChanges[T] was not called first.
func QueryChanged1[T any](ctx *Ctx, fn func(ecs.Entity, *T)) {
	buf, ok := ctx.app.changeBuffers[typeKey[T]()]
	if !ok {
		panic(fmt.Sprintf("QueryChanged1[%s]: change tracking not enabled — call app.TrackChanges[%s](a) at Build time",
			reflect.TypeOf((*T)(nil)).Elem(), reflect.TypeOf((*T)(nil)).Elem()))
	}
	if len(buf.entities) == 0 {
		return
	}
	m := generic.NewMap1[T](ctx.world)
	// Iterate a snapshot — fn may indirectly cause more entities to be added
	// to buf (e.g. via Commands that spawn T-bearing entities), and we want
	// those to land in next frame's set, not this iteration.
	n := len(buf.entities)
	for i := 0; i < n; i++ {
		e := buf.entities[i]
		if !ctx.world.Alive(e) {
			continue
		}
		// Entity may have lost the component between mark and query; Map1.Get
		// returns nil in that case (it wraps World.Get which returns nil for
		// absent components). Skip rather than dereference.
		p := m.Get(e)
		if p == nil {
			continue
		}
		fn(e, p)
	}
}

// MarkChanged adds entity e to the current frame's change set for T.
// Use it after writing through a *T pointer obtained from a Query — Go can't
// intercept the write, so the writer flags it explicitly.
//
// Panics if TrackChanges[T] was not called first.
func MarkChanged[T any](ctx *Ctx, e ecs.Entity) {
	buf, ok := ctx.app.changeBuffers[typeKey[T]()]
	if !ok {
		panic(fmt.Sprintf("MarkChanged[%s]: change tracking not enabled — call app.TrackChanges[%s](a) at Build time",
			reflect.TypeOf((*T)(nil)).Elem(), reflect.TypeOf((*T)(nil)).Elem()))
	}
	buf.add(e)
}

// drainAllChanges clears every per-type change buffer. Called by the App at
// the end of each frame, alongside drainAllEvents.
func (a *App) drainAllChanges() {
	for _, buf := range a.changeBuffers {
		buf.drain()
	}
}
