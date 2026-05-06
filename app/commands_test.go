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
