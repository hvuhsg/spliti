// Package components is the editor's component registry: a flat list
// of ComponentDesc entries, one per built-in component, that drives:
//
//   - Inspector rendering (one block per component)
//   - Inspector editing (per-field key handlers)
//   - Scene save (encode) and load (decode)
//   - Snapshot/restore (encode + decode for play/stop)
//   - "Add component" / "Remove component" inspector menus
//
// The registry is set up by Init() at editor-plugin startup. Call sites
// that need it use Registry() to get the current list.
package components

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/mlange-42/arche/ecs"
)

// FieldKind classifies a field's editor widget. The inspector's
// keypress handler dispatches per kind: Int adopts +/-/digits, Bool
// adopts space-toggle, etc.
type FieldKind int

const (
	FieldInt FieldKind = iota
	FieldString
	FieldBool
	FieldRune
	FieldStyle        // shown read-only summary in v1
	FieldKeyBindings  // shown read-only summary in v1
)

// Field is one editable property on a component. Get returns the current
// value (boxed in any per Kind); Set writes a new value (caller's job to
// validate type before calling).
type Field struct {
	Name string
	Kind FieldKind
	Get  func(c *app.Ctx, e ecs.Entity) any
	Set  func(c *app.Ctx, e ecs.Entity, v any)
}

// ComponentDesc is the per-component contract. Encode produces a TOML
// sub-table value (map[string]any); Decode applies one to an entity.
// The save/load machinery and the snapshot/restore machinery both go
// through these functions, so they're a single source of truth.
type ComponentDesc struct {
	Name   string
	Has    func(w *ecs.World, e ecs.Entity) bool
	Add    func(c *app.Ctx, e ecs.Entity)
	Remove func(c *app.Ctx, e ecs.Entity)
	Fields []Field
	Encode func(w *ecs.World, e ecs.Entity) (map[string]any, bool)
	Decode func(c *app.Ctx, e ecs.Entity, val map[string]any) error
}

// registry holds every ComponentDesc the editor knows about. Order
// matters: Inspector renders top-down, save outputs in this order.
var registry []*ComponentDesc

// Register adds a desc to the global registry. Duplicates by Name are
// rejected with a panic — this is set-up-once code, not user data.
func Register(d *ComponentDesc) {
	for _, existing := range registry {
		if existing.Name == d.Name {
			panic("components.Register: duplicate name " + d.Name)
		}
	}
	registry = append(registry, d)
}

// Registry returns the current list. Callers MUST NOT mutate the returned
// slice — it's the live registry.
func Registry() []*ComponentDesc { return registry }

// ByName looks up a desc by component name, or nil if not registered.
// The scene loader and snapshot restore both use this to map TOML keys
// back to component handlers.
func ByName(name string) *ComponentDesc {
	for _, d := range registry {
		if d.Name == name {
			return d
		}
	}
	return nil
}

// Reset clears the registry. Used by tests that need a clean slate; not
// meant for production code.
func Reset() { registry = nil }

// Unregister removes the desc with the given name. Used when custom
// component schemas are edited at runtime: the editor first
// unregisters the old custom descs, then RegisterCustom re-adds them
// from the updated schema list.
//
// Returns true if a desc was removed.
func Unregister(name string) bool {
	for i, d := range registry {
		if d.Name == name {
			registry = append(registry[:i], registry[i+1:]...)
			return true
		}
	}
	return false
}
