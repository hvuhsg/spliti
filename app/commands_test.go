package app_test

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
)

type Position struct{ X, Y int }
type Velocity struct{ X, Y int }
type Marker struct{}

func TestSpawnCommandCreatesEntity(t *testing.T) {
	var seen []Position
	app.New().
		AddSystems(schedule.Startup, func(c *app.Ctx) {
			app.Spawn1(c.Commands(), func(p *Position) { p.X = 7; p.Y = 9 })
		}).
		AddSystems(schedule.Update, func(c *app.Ctx) {
			app.Query1(c, func(_ ecs.Entity, p *Position) {
				seen = append(seen, *p)
			})
		}).
		SetMaxFrames(1).
		Run()

	if len(seen) != 1 || seen[0] != (Position{X: 7, Y: 9}) {
		t.Fatalf("got %+v, want one Position{7,9}", seen)
	}
}

func TestSpawn2CreatesEntityWithTwoComponents(t *testing.T) {
	var pCount, vCount int
	app.New().
		AddSystems(schedule.Startup, func(c *app.Ctx) {
			app.Spawn2(c.Commands(), func(p *Position, v *Velocity) {
				p.X, p.Y = 1, 2
				v.X, v.Y = 3, 4
			})
		}).
		AddSystems(schedule.Update, func(c *app.Ctx) {
			app.Query2[Position, Velocity](c, func(_ ecs.Entity, _ *Position, _ *Velocity) {
				pCount++
				vCount++
			})
		}).
		SetMaxFrames(1).
		Run()

	if pCount != 1 || vCount != 1 {
		t.Fatalf("p=%d v=%d, want both 1", pCount, vCount)
	}
}

func TestSpawn6CreatesEntityQueryableAt5And6(t *testing.T) {
	type C1 struct{ V int }
	type C2 struct{ V int }
	type C3 struct{ V int }
	type C4 struct{ V int }
	type C5 struct{ V int }
	type C6 struct{ V int }

	var got5, got6 int
	var sum int
	app.New().
		AddSystems(schedule.Startup, func(c *app.Ctx) {
			app.Spawn6(c.Commands(), func(a *C1, b *C2, cc *C3, d *C4, e *C5, f *C6) {
				a.V, b.V, cc.V, d.V, e.V, f.V = 1, 2, 3, 4, 5, 6
			})
			app.Spawn5(c.Commands(), func(a *C1, b *C2, cc *C3, d *C4, e *C5) {
				a.V, b.V, cc.V, d.V, e.V = 10, 20, 30, 40, 50
			})
		}).
		AddSystems(schedule.Update, func(c *app.Ctx) {
			app.Query5[C1, C2, C3, C4, C5](c, func(_ ecs.Entity, _ *C1, _ *C2, _ *C3, _ *C4, _ *C5) {
				got5++
			})
			app.Query6(c, func(_ ecs.Entity, a *C1, b *C2, cc *C3, d *C4, e *C5, f *C6) {
				got6++
				sum = a.V + b.V + cc.V + d.V + e.V + f.V
			})
		}).
		SetMaxFrames(1).
		Run()

	if got5 != 2 { // both entities have the first five components
		t.Fatalf("Query5 matched %d entities, want 2", got5)
	}
	if got6 != 1 || sum != 21 {
		t.Fatalf("Query6 matched %d entities (component sum %d), want 1 with sum 21", got6, sum)
	}
}

func TestDespawnCommandRemovesEntity(t *testing.T) {
	var liveAfterDespawn int
	app.New().
		AddSystems(schedule.Startup, func(c *app.Ctx) {
			app.Spawn1(c.Commands(), func(*Marker) {})
			app.Spawn1(c.Commands(), func(*Marker) {})
			app.Spawn1(c.Commands(), func(*Marker) {})
		}).
		AddSystems(schedule.Update, app.System(func(c *app.Ctx) {
			var first ecs.Entity
			app.Query1[Marker](c, func(e ecs.Entity, _ *Marker) {
				if first.IsZero() {
					first = e
				}
			})
			c.Commands().Despawn(first)
		}).Label("kill_one")).
		AddSystems(schedule.PostUpdate, app.System(func(c *app.Ctx) {
			app.Query1[Marker](c, func(ecs.Entity, *Marker) { liveAfterDespawn++ })
		})).
		SetMaxFrames(1).
		Run()

	if liveAfterDespawn != 2 {
		t.Fatalf("expected 2 entities to remain, got %d", liveAfterDespawn)
	}
}
