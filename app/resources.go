package app

import "reflect"

// EditorMode is a marker resource the editor inserts when it hosts a game in
// play mode. Game code can detect it (GetResource[EditorMode](c) != nil) to
// disable behavior that must not run inside the editor, such as capturing the
// mouse cursor for FPS mouselook. It is absent in a standalone run.
type EditorMode struct{}

// InsertResource stores a singleton value of type T on the App. Resources are
// keyed by reflect.Type — one per type. Re-inserting overwrites.
func InsertResource[T any](a *App, v *T) {
	a.resources[typeKey[T]()] = v
}

// GetResource returns the resource of type T, or nil if none was inserted.
func GetResource[T any](ctx *Ctx) *T {
	if v, ok := ctx.app.resources[typeKey[T]()]; ok {
		return v.(*T)
	}
	return nil
}

// HasResource reports whether a resource of type T has been inserted.
func HasResource[T any](a *App) bool {
	_, ok := a.resources[typeKey[T]()]
	return ok
}

// Resources returns a snapshot of every installed resource keyed by its type,
// for inspection and debugging (see package inspect). The map is a fresh copy;
// the values alias the live resources. Prefer GetResource in normal code — this
// exists so tooling can enumerate resources without knowing their types.
func (a *App) Resources() map[reflect.Type]any {
	out := make(map[reflect.Type]any, len(a.resources))
	for k, v := range a.resources {
		out[k] = v
	}
	return out
}

func typeKey[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}
