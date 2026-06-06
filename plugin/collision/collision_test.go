package collision

import (
	"math/rand"
	"sort"
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// driveFixed runs one frame with FixedUpdate executed exactly once, mirroring
// the runtime package's test harness (the time plugin is avoided so the test
// isn't tied to wall-clock pacing).
func driveFixed(a *app.App) {
	a.SetMaxFrames(1)
	a.SetPreUpdateHook(func() { a.RunStage(schedule.FixedUpdate) })
	a.Run()
}

// collectEvents installs a Last-stage system that captures every CollisionEvent
// emitted this frame into *out.
func collectEvents(a *app.App, out *[]CollisionEvent) {
	a.AddSystems(schedule.Last, func(c *app.Ctx) {
		*out = append(*out, app.ReadEvents[CollisionEvent](c)...)
	})
}

func TestSystem_OverlappingBoxesEmitOnePair(t *testing.T) {
	a := app.New()
	a.AddPlugins(Plugin{})
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap2[tui.Position, Collider](c.World())
		m.NewWith(&tui.Position{X: 0, Y: 0}, &Collider{W: 3, H: 3})
		m.NewWith(&tui.Position{X: 2, Y: 2}, &Collider{W: 3, H: 3})
		m.NewWith(&tui.Position{X: 100, Y: 100}, &Collider{W: 1, H: 1})
	})
	var got []CollisionEvent
	collectEvents(a, &got)
	driveFixed(a)
	if len(got) != 1 {
		t.Fatalf("expected 1 collision event, got %d: %+v", len(got), got)
	}
}

func TestSystem_TouchingFacesDoNotCollide(t *testing.T) {
	a := app.New()
	a.AddPlugins(Plugin{})
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap2[tui.Position, Collider](c.World())
		// b starts exactly where a ends on X: [0,3) and [3,6) only touch.
		m.NewWith(&tui.Position{X: 0, Y: 0}, &Collider{W: 3, H: 3})
		m.NewWith(&tui.Position{X: 3, Y: 0}, &Collider{W: 3, H: 3})
	})
	var got []CollisionEvent
	collectEvents(a, &got)
	driveFixed(a)
	if len(got) != 0 {
		t.Fatalf("touching faces should not collide, got %d events", len(got))
	}
}

func TestSystem_TagResolver(t *testing.T) {
	type named struct{ Name string }
	a := app.New()
	a.AddPlugins(Plugin{Config: Config{
		Tag: func(w *ecs.World, e ecs.Entity) string {
			id := ecs.ComponentID[named](w)
			if w.Has(e, id) {
				m := generic.NewMap1[named](w)
				return m.Get(e).Name
			}
			return ""
		},
	}})
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap3[tui.Position, Collider, named](c.World())
		m.NewWith(&tui.Position{X: 0, Y: 0}, &Collider{W: 3, H: 3}, &named{Name: "a"})
		m.NewWith(&tui.Position{X: 1, Y: 1}, &Collider{W: 3, H: 3}, &named{Name: "b"})
	})
	var got []CollisionEvent
	collectEvents(a, &got)
	driveFixed(a)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if !((got[0].ATag == "a" && got[0].BTag == "b") || (got[0].ATag == "b" && got[0].BTag == "a")) {
		t.Fatalf("tags not resolved: %+v", got[0])
	}
}

func TestPlugin_RunIfFalse_Suppresses(t *testing.T) {
	a := app.New()
	a.AddPlugins(Plugin{RunIf: func(*app.Ctx) bool { return false }})
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap2[tui.Position, Collider](c.World())
		m.NewWith(&tui.Position{X: 0, Y: 0}, &Collider{W: 3, H: 3})
		m.NewWith(&tui.Position{X: 1, Y: 1}, &Collider{W: 3, H: 3})
	})
	var got []CollisionEvent
	collectEvents(a, &got)
	driveFixed(a)
	if len(got) != 0 {
		t.Fatalf("RunIf=false should suppress collision, got %d events", len(got))
	}
}

func TestLayers(t *testing.T) {
	const (
		layerA uint32 = 1 << 0
		layerB uint32 = 1 << 1
	)
	cases := []struct {
		name string
		a, b Collider
		want bool
	}{
		{"zero values collide", Collider{}, Collider{}, true},
		{"same layer, masks match", Collider{Layer: layerA, Mask: layerA}, Collider{Layer: layerA, Mask: layerA}, true},
		{"disjoint masks miss", Collider{Layer: layerA, Mask: layerA}, Collider{Layer: layerB, Mask: layerB}, false},
		{"one-directional mask misses", Collider{Layer: layerA, Mask: layerB}, Collider{Layer: layerB, Mask: layerB}, false},
		{"bidirectional masks hit", Collider{Layer: layerA, Mask: layerB}, Collider{Layer: layerB, Mask: layerA}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collides(tc.a.Layer, tc.a.Mask, tc.b.Layer, tc.b.Mask)
			if got != tc.want {
				t.Fatalf("collides = %v, want %v", got, tc.want)
			}
		})
	}
}

// bruteForce2 returns every overlapping, layer-compatible pair (i<j) by the
// naive O(n²) scan — the ground truth the broad phase must reproduce exactly.
func bruteForce2(bodies []aabb2) [][2]int {
	var out [][2]int
	for i := 0; i < len(bodies); i++ {
		for j := i + 1; j < len(bodies); j++ {
			if overlap2(bodies[i], bodies[j]) && collides(bodies[i].layer, bodies[i].mask, bodies[j].layer, bodies[j].mask) {
				out = append(out, [2]int{i, j})
			}
		}
	}
	return out
}

func collectPairs2(bodies []aabb2, cell int) [][2]int {
	var out [][2]int
	pairs2(bodies, cell, func(i, j int) { out = append(out, [2]int{i, j}) })
	return out
}

func TestBroadphaseMatchesBruteForce2D(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	layers := []uint32{0, 1, 2, 3} // includes the "collide with all" zero value
	for iter := 0; iter < 200; iter++ {
		n := rng.Intn(40)
		bodies := make([]aabb2, n)
		for i := range bodies {
			x := rng.Intn(60) - 20 // spans negative coordinates
			y := rng.Intn(60) - 20
			w := rng.Intn(5) + 1
			h := rng.Intn(5) + 1
			bodies[i] = aabb2{
				minX: x, minY: y, maxX: x + w, maxY: y + h,
				layer: layers[rng.Intn(len(layers))],
				mask:  layers[rng.Intn(len(layers))],
			}
		}
		want := bruteForce2(bodies)
		// The broad phase must be invariant to cell size.
		for _, cell := range []int{1, 2, 3, 8, 64, 1000} {
			got := collectPairs2(bodies, cell)
			if !equalPairs(got, want) {
				t.Fatalf("iter %d cell %d: broadphase mismatch\n got=%v\nwant=%v", iter, cell, got, want)
			}
		}
	}
}

// equalPairs reports whether two pair lists are identical, including order
// (the broad phase is required to emit in ascending i,j order like the scan).
func equalPairs(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPairs2_AscendingOrder(t *testing.T) {
	// Three mutually overlapping boxes → pairs must be (0,1),(0,2),(1,2).
	bodies := []aabb2{
		{minX: 0, minY: 0, maxX: 5, maxY: 5},
		{minX: 1, minY: 1, maxX: 6, maxY: 6},
		{minX: 2, minY: 2, maxX: 7, maxY: 7},
	}
	got := collectPairs2(bodies, 2)
	want := [][2]int{{0, 1}, {0, 2}, {1, 2}}
	if !sortedEqual(got, want) || !equalPairs(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// sortedEqual checks set equality after sorting (used alongside equalPairs to
// make the intent of an ordered comparison explicit).
func sortedEqual(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	cp := func(s [][2]int) [][2]int {
		d := append([][2]int(nil), s...)
		sort.Slice(d, func(i, j int) bool {
			if d[i][0] != d[j][0] {
				return d[i][0] < d[j][0]
			}
			return d[i][1] < d[j][1]
		})
		return d
	}
	sa, sb := cp(a), cp(b)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}
