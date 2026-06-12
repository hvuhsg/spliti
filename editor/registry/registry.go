// Package registry gives the editor reflective access to component types. The
// ECS stores components by Go type; the inspector needs to enumerate, read,
// and write them without knowing the types at compile time. Register[T] bridges
// the two: generics capture the arche storage accessors once per type, and
// reflect exposes the component's fields to the UI.
//
// The set of registrations for a game project is generated into
// .spliti/editor/registry_gen.go by `spliti gen` (every exported struct in the
// game's components package plus the engine built-ins); Builtin covers the
// engine side.
package registry

import (
	"reflect"
	"sort"

	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Registry is the set of component types the editor can inspect.
type Registry struct {
	byName map[string]*TypeInfo
	sorted []*TypeInfo
}

// TypeInfo describes one registered component type, with closures over the
// concrete type for world access.
type TypeInfo struct {
	Name string // unqualified type name, e.g. "Health"
	Pkg  string // package qualifier as written in scene files, e.g. "components"
	Type reflect.Type

	// Hidden components exist (and are inspectable in principle) but are
	// engine plumbing the inspector should not list, e.g. GlobalTransform.
	Hidden bool

	Has    func(w *ecs.World, e ecs.Entity) bool
	Remove func(w *ecs.World, e ecs.Entity)
	// Get returns the live component as an addressable reflect.Value — writes
	// through it mutate the world directly. Only valid while the entity keeps
	// its archetype (re-fetch after any structural change).
	Get func(w *ecs.World, e ecs.Entity) reflect.Value
	// Add attaches the type's zero-ish default to the entity.
	Add func(w *ecs.World, e ecs.Entity)
	// Value returns a copy of the live component (components are plain structs,
	// so a value copy is a deep copy) — undo snapshots use this.
	Value func(w *ecs.World, e ecs.Entity) any
	// SetValue writes a captured value back, attaching the component first when
	// absent. Values of the wrong dynamic type are ignored.
	SetValue func(w *ecs.World, e ecs.Entity, val any)

	Fields []Field
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{byName: make(map[string]*TypeInfo)}
}

// Register adds component type T under the given display name and package
// qualifier. Registering the same name twice keeps the first registration.
func Register[T any](r *Registry, name, pkg string) {
	if _, ok := r.byName[name]; ok {
		return
	}
	rt := reflect.TypeFor[T]()
	ti := &TypeInfo{
		Name: name,
		Pkg:  pkg,
		Type: rt,
		Has: func(w *ecs.World, e ecs.Entity) bool {
			mp := generic.NewMap[T](w)
			return mp.Has(e)
		},
		Get: func(w *ecs.World, e ecs.Entity) reflect.Value {
			mp := generic.NewMap[T](w)
			return reflect.ValueOf(mp.Get(e)).Elem()
		},
		Add: func(w *ecs.World, e ecs.Entity) {
			var v T
			mp := generic.NewMap1[T](w)
			mp.Assign(e, &v)
		},
		Remove: func(w *ecs.World, e ecs.Entity) {
			mp := generic.NewMap1[T](w)
			mp.Remove(e)
		},
		Value: func(w *ecs.World, e ecs.Entity) any {
			mp := generic.NewMap[T](w)
			return *mp.Get(e)
		},
		SetValue: func(w *ecs.World, e ecs.Entity, val any) {
			v, ok := val.(T)
			if !ok {
				return
			}
			mp := generic.NewMap[T](w)
			if mp.Has(e) {
				mp.Set(e, &v)
				return
			}
			m1 := generic.NewMap1[T](w)
			m1.Assign(e, &v)
		},
		Fields: walkFields(rt),
	}
	r.byName[name] = ti
	r.sorted = append(r.sorted, ti)
	sort.Slice(r.sorted, func(i, j int) bool { return r.sorted[i].Name < r.sorted[j].Name })
}

// Hide marks an already-registered type as hidden in the inspector.
func (r *Registry) Hide(name string) {
	if ti := r.byName[name]; ti != nil {
		ti.Hidden = true
	}
}

// Lookup returns the TypeInfo registered under name, or nil.
func (r *Registry) Lookup(name string) *TypeInfo { return r.byName[name] }

// Types returns all registered types sorted by name.
func (r *Registry) Types() []*TypeInfo { return r.sorted }

// On returns the visible component types present on e, sorted by name.
func (r *Registry) On(w *ecs.World, e ecs.Entity) []*TypeInfo {
	var out []*TypeInfo
	for _, ti := range r.sorted {
		if !ti.Hidden && ti.Has(w, e) {
			out = append(out, ti)
		}
	}
	return out
}
