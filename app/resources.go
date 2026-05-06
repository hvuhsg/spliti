package app

import "reflect"

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

func typeKey[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}
