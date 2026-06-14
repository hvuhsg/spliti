package app

import (
	"reflect"

	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// cachedFilter returns the Ctx-cached filter for key, building it with mk on
// first use. Reusing the filter avoids per-call allocation and recompilation;
// see the note on Ctx.filters for why the cache is per-Ctx (per-world).
func cachedFilter[F any](c *Ctx, key filterKey, mk func() F) F {
	c.filterMu.Lock()
	defer c.filterMu.Unlock()
	if c.filters == nil {
		c.filters = make(map[filterKey]any)
	}
	if v, ok := c.filters[key]; ok {
		return v.(F)
	}
	f := mk()
	c.filters[key] = f
	return f
}

// cachedMap1 returns the Ctx-cached generic Map for component T, building it on
// first use. Like filters, Maps embed per-world component IDs, so the cache is
// per-Ctx (Ctx.world is fixed for the Ctx's lifetime).
func cachedMap1[T any](c *Ctx) generic.Map1[T] {
	key := filterKey{reflect.TypeFor[T]()}
	c.filterMu.Lock()
	defer c.filterMu.Unlock()
	if c.maps == nil {
		c.maps = make(map[filterKey]any)
	}
	if v, ok := c.maps[key]; ok {
		return v.(generic.Map1[T])
	}
	m := generic.NewMap1[T](c.world)
	c.maps[key] = m
	return m
}

// Query1 iterates all entities that have component A.
func Query1[A any](ctx *Ctx, fn func(ecs.Entity, *A)) {
	f := cachedFilter(ctx, filterKey{reflect.TypeFor[A]()}, generic.NewFilter1[A])
	q := f.Query(ctx.world)
	for q.Next() {
		fn(q.Entity(), q.Get())
	}
}

// Query2 iterates all entities that have components A and B.
func Query2[A, B any](ctx *Ctx, fn func(ecs.Entity, *A, *B)) {
	f := cachedFilter(ctx, filterKey{reflect.TypeFor[A](), reflect.TypeFor[B]()}, generic.NewFilter2[A, B])
	q := f.Query(ctx.world)
	for q.Next() {
		a, b := q.Get()
		fn(q.Entity(), a, b)
	}
}

// Query3 iterates all entities that have components A, B and C.
func Query3[A, B, C any](ctx *Ctx, fn func(ecs.Entity, *A, *B, *C)) {
	f := cachedFilter(ctx, filterKey{reflect.TypeFor[A](), reflect.TypeFor[B](), reflect.TypeFor[C]()}, generic.NewFilter3[A, B, C])
	q := f.Query(ctx.world)
	for q.Next() {
		a, b, c := q.Get()
		fn(q.Entity(), a, b, c)
	}
}

// Query4 iterates all entities that have components A, B, C and D.
func Query4[A, B, C, D any](ctx *Ctx, fn func(ecs.Entity, *A, *B, *C, *D)) {
	f := cachedFilter(ctx, filterKey{reflect.TypeFor[A](), reflect.TypeFor[B](), reflect.TypeFor[C](), reflect.TypeFor[D]()}, generic.NewFilter4[A, B, C, D])
	q := f.Query(ctx.world)
	for q.Next() {
		a, b, c, d := q.Get()
		fn(q.Entity(), a, b, c, d)
	}
}

// Query5 iterates all entities that have components A, B, C, D and E.
func Query5[A, B, C, D, E any](ctx *Ctx, fn func(ecs.Entity, *A, *B, *C, *D, *E)) {
	f := cachedFilter(ctx, filterKey{reflect.TypeFor[A](), reflect.TypeFor[B](), reflect.TypeFor[C](), reflect.TypeFor[D](), reflect.TypeFor[E]()}, generic.NewFilter5[A, B, C, D, E])
	q := f.Query(ctx.world)
	for q.Next() {
		a, b, c, d, e := q.Get()
		fn(q.Entity(), a, b, c, d, e)
	}
}

// Query6 iterates all entities that have components A, B, C, D, E and F.
func Query6[A, B, C, D, E, F any](ctx *Ctx, fn func(ecs.Entity, *A, *B, *C, *D, *E, *F)) {
	flt := cachedFilter(ctx, filterKey{reflect.TypeFor[A](), reflect.TypeFor[B](), reflect.TypeFor[C](), reflect.TypeFor[D](), reflect.TypeFor[E](), reflect.TypeFor[F]()}, generic.NewFilter6[A, B, C, D, E, F])
	q := flt.Query(ctx.world)
	for q.Next() {
		a, b, c, d, e, f := q.Get()
		fn(q.Entity(), a, b, c, d, e, f)
	}
}
