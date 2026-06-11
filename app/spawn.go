package app

import (
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Spawn1 queues entity creation with one component. The init callback runs at
// apply time with the freshly-allocated component.
func Spawn1[A any](c *Commands, init func(*A)) {
	c.Add(func(w *ecs.World) {
		m := generic.NewMap1[A](w)
		e := m.New()
		if init != nil {
			init(m.Get(e))
		}
	})
}

// Spawn2 queues entity creation with two components.
func Spawn2[A, B any](c *Commands, init func(*A, *B)) {
	c.Add(func(w *ecs.World) {
		m := generic.NewMap2[A, B](w)
		e := m.New()
		if init != nil {
			a, b := m.Get(e)
			init(a, b)
		}
	})
}

// Spawn3 queues entity creation with three components.
func Spawn3[A, B, C any](c *Commands, init func(*A, *B, *C)) {
	c.Add(func(w *ecs.World) {
		m := generic.NewMap3[A, B, C](w)
		e := m.New()
		if init != nil {
			a, b, c := m.Get(e)
			init(a, b, c)
		}
	})
}

// Spawn4 queues entity creation with four components.
func Spawn4[A, B, C, D any](c *Commands, init func(*A, *B, *C, *D)) {
	c.Add(func(w *ecs.World) {
		m := generic.NewMap4[A, B, C, D](w)
		e := m.New()
		if init != nil {
			a, b, c, d := m.Get(e)
			init(a, b, c, d)
		}
	})
}

// Spawn5 queues entity creation with five components.
func Spawn5[A, B, C, D, E any](c *Commands, init func(*A, *B, *C, *D, *E)) {
	c.Add(func(w *ecs.World) {
		m := generic.NewMap5[A, B, C, D, E](w)
		ent := m.New()
		if init != nil {
			a, b, c, d, e := m.Get(ent)
			init(a, b, c, d, e)
		}
	})
}

// Spawn6 queues entity creation with six components.
func Spawn6[A, B, C, D, E, F any](c *Commands, init func(*A, *B, *C, *D, *E, *F)) {
	c.Add(func(w *ecs.World) {
		m := generic.NewMap6[A, B, C, D, E, F](w)
		ent := m.New()
		if init != nil {
			a, b, c, d, e, f := m.Get(ent)
			init(a, b, c, d, e, f)
		}
	})
}
