package collision

import (
	"math/rand"
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

func TestSystem3D_OverlappingBoxesEmitOnePair(t *testing.T) {
	// Position comes from a Position3 component the test owns, so the collision
	// package never needs the renderer.
	type Position3 struct{ V m.Vec3 }
	a := app.New()
	a.AddPlugins(Plugin3D{Config3D: Config3D{
		Pos: func(w *ecs.World, e ecs.Entity) m.Vec3 {
			mp := generic.NewMap1[Position3](w)
			return mp.Get(e).V
		},
	}})
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		mp := generic.NewMap2[Position3, Collider3D](c.World())
		half := m.Vec3{X: 1, Y: 1, Z: 1}
		mp.NewWith(&Position3{V: m.Vec3{X: 0, Y: 0, Z: 0}}, &Collider3D{Half: half})
		mp.NewWith(&Position3{V: m.Vec3{X: 1, Y: 1, Z: 1}}, &Collider3D{Half: half})   // overlaps
		mp.NewWith(&Position3{V: m.Vec3{X: 50, Y: 0, Z: 0}}, &Collider3D{Half: half}) // far away
	})
	var got []Collision3DEvent
	a.AddSystems(schedule.Last, func(c *app.Ctx) {
		got = append(got, app.ReadEvents[Collision3DEvent](c)...)
	})
	driveFixed(a)
	if len(got) != 1 {
		t.Fatalf("expected 1 collision event, got %d: %+v", len(got), got)
	}
}

func TestNewSystem3D_NilPosPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when Config3D.Pos is nil")
		}
	}()
	NewSystem3D(Config3D{})
}

func bruteForce3(bodies []aabb3) [][2]int {
	var out [][2]int
	for i := 0; i < len(bodies); i++ {
		for j := i + 1; j < len(bodies); j++ {
			if overlap3(bodies[i], bodies[j]) && collides(bodies[i].layer, bodies[i].mask, bodies[j].layer, bodies[j].mask) {
				out = append(out, [2]int{i, j})
			}
		}
	}
	return out
}

func collectPairs3(bodies []aabb3, cell float32) [][2]int {
	var out [][2]int
	pairs3(bodies, cell, func(i, j int) { out = append(out, [2]int{i, j}) })
	return out
}

func TestBroadphaseMatchesBruteForce3D(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	layers := []uint32{0, 1, 2, 3}
	for iter := 0; iter < 200; iter++ {
		n := rng.Intn(40)
		bodies := make([]aabb3, n)
		for i := range bodies {
			c := m.Vec3{
				X: rng.Float32()*40 - 20,
				Y: rng.Float32()*40 - 20,
				Z: rng.Float32()*40 - 20,
			}
			h := m.Vec3{
				X: rng.Float32()*2 + 0.5,
				Y: rng.Float32()*2 + 0.5,
				Z: rng.Float32()*2 + 0.5,
			}
			bodies[i] = aabb3{
				min:   c.Sub(h),
				max:   c.Add(h),
				layer: layers[rng.Intn(len(layers))],
				mask:  layers[rng.Intn(len(layers))],
			}
		}
		want := bruteForce3(bodies)
		for _, cell := range []float32{0.5, 1, 4, 32, 500} {
			got := collectPairs3(bodies, cell)
			if !equalPairs(got, want) {
				t.Fatalf("iter %d cell %g: broadphase mismatch\n got=%v\nwant=%v", iter, cell, got, want)
			}
		}
	}
}
