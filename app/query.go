package app

import (
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Query1 iterates all entities that have component A.
func Query1[A any](ctx *Ctx, fn func(ecs.Entity, *A)) {
	f := generic.NewFilter1[A]()
	q := f.Query(ctx.world)
	for q.Next() {
		fn(q.Entity(), q.Get())
	}
}

// Query2 iterates all entities that have components A and B.
func Query2[A, B any](ctx *Ctx, fn func(ecs.Entity, *A, *B)) {
	f := generic.NewFilter2[A, B]()
	q := f.Query(ctx.world)
	for q.Next() {
		a, b := q.Get()
		fn(q.Entity(), a, b)
	}
}

// Query3 iterates all entities that have components A, B and C.
func Query3[A, B, C any](ctx *Ctx, fn func(ecs.Entity, *A, *B, *C)) {
	f := generic.NewFilter3[A, B, C]()
	q := f.Query(ctx.world)
	for q.Next() {
		a, b, c := q.Get()
		fn(q.Entity(), a, b, c)
	}
}

// Query4 iterates all entities that have components A, B, C and D.
func Query4[A, B, C, D any](ctx *Ctx, fn func(ecs.Entity, *A, *B, *C, *D)) {
	f := generic.NewFilter4[A, B, C, D]()
	q := f.Query(ctx.world)
	for q.Next() {
		a, b, c, d := q.Get()
		fn(q.Entity(), a, b, c, d)
	}
}

// Query5 iterates all entities that have components A, B, C, D and E.
func Query5[A, B, C, D, E any](ctx *Ctx, fn func(ecs.Entity, *A, *B, *C, *D, *E)) {
	f := generic.NewFilter5[A, B, C, D, E]()
	q := f.Query(ctx.world)
	for q.Next() {
		a, b, c, d, e := q.Get()
		fn(q.Entity(), a, b, c, d, e)
	}
}

// Query6 iterates all entities that have components A, B, C, D, E and F.
func Query6[A, B, C, D, E, F any](ctx *Ctx, fn func(ecs.Entity, *A, *B, *C, *D, *E, *F)) {
	flt := generic.NewFilter6[A, B, C, D, E, F]()
	q := flt.Query(ctx.world)
	for q.Next() {
		a, b, c, d, e, f := q.Get()
		fn(q.Entity(), a, b, c, d, e, f)
	}
}
