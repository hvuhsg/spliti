package components_test

import (
	"sync"
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/components"
	"github.com/hvuhsg/spliti/plugin/runtime"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// We share the registry across tests in this package, so initialise
// exactly once. RegisterBuiltins panics on a second call — that's the
// guard against accidental double-registration.
var initOnce sync.Once

func ensureRegistry() {
	initOnce.Do(func() {
		components.Reset()
		components.RegisterBuiltins()
	})
}

func TestRegistry_Position_RoundTrip(t *testing.T) {
	ensureRegistry()
	a := app.New()
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap1[tui.Position](c.World())
		e := m.New()
		*m.Get(e) = tui.Position{X: 7, Y: 9}
	})
	a.SetMaxFrames(1).Run()

	desc := components.ByName("position")
	if desc == nil {
		t.Fatal("position not registered")
	}
	w := a.Ctx().World()
	var enc map[string]any
	app.Query1[tui.Position](a.Ctx(), func(e ecs.Entity, _ *tui.Position) {
		val, ok := desc.Encode(w, e)
		if !ok {
			t.Fatal("encode false")
		}
		enc = val
	})
	if enc["x"].(int64) != 7 || enc["y"].(int64) != 9 {
		t.Fatalf("encoded = %+v", enc)
	}

	// Decode into a NEW entity and verify values transfer.
	a2 := app.New()
	var newE ecs.Entity
	a2.AddSystems(schedule.Startup, func(c *app.Ctx) {
		newE = c.World().NewEntity()
		if err := desc.Decode(c, newE, enc); err != nil {
			t.Fatalf("decode: %v", err)
		}
	})
	a2.SetMaxFrames(1).Run()
	posMap := generic.NewMap1[tui.Position](a2.Ctx().World())
	pos := posMap.Get(newE)
	if pos.X != 7 || pos.Y != 9 {
		t.Fatalf("decoded position = %+v, want {7,9}", pos)
	}
}

func TestRegistry_Velocity_AddRemoveHas(t *testing.T) {
	ensureRegistry()
	a := app.New()
	desc := components.ByName("velocity")
	if desc == nil {
		t.Fatal("velocity not registered")
	}
	var e ecs.Entity
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		e = c.World().NewEntity()
		if desc.Has(c.World(), e) {
			t.Fatal("brand-new entity should NOT have velocity")
		}
		desc.Add(c, e)
		if !desc.Has(c.World(), e) {
			t.Fatal("Add(velocity) did not produce Has()")
		}
		// Mutate via Field.Set, read via Field.Get.
		desc.Fields[0].Set(c, e, 5)
		if got := desc.Fields[0].Get(c, e); got.(int) != 5 {
			t.Fatalf("Field DX get = %v, want 5", got)
		}
		desc.Remove(c, e)
		if desc.Has(c.World(), e) {
			t.Fatal("Remove(velocity) failed")
		}
	})
	a.SetMaxFrames(1).Run()
}

func TestRegistry_KeyboardControl_BindingsRoundTrip(t *testing.T) {
	ensureRegistry()
	a := app.New()
	desc := components.ByName("keyboardControl")
	if desc == nil {
		t.Fatal("keyboardControl not registered")
	}
	var e ecs.Entity
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap1[runtime.KeyboardControl](c.World())
		e = m.New()
		kc := m.Get(e)
		kc.Bindings = []runtime.KeyBinding{{
			Rune: 'd', DX: 1, DY: 0, HoldTicks: 4,
		}}
	})
	a.SetMaxFrames(1).Run()

	w := a.Ctx().World()
	enc, ok := desc.Encode(w, e)
	if !ok {
		t.Fatal("encode false")
	}
	bindings, ok := enc["bindings"].([]map[string]any)
	if !ok || len(bindings) != 1 {
		t.Fatalf("bindings = %v", enc["bindings"])
	}
	if bindings[0]["dx"].(int64) != 1 || bindings[0]["holdTicks"].(int64) != 4 {
		t.Fatalf("encoded binding = %+v", bindings[0])
	}

	// Decode into a fresh entity and verify the slice round-trips.
	a2 := app.New()
	var e2 ecs.Entity
	a2.AddSystems(schedule.Startup, func(c *app.Ctx) {
		e2 = c.World().NewEntity()
		if err := desc.Decode(c, e2, enc); err != nil {
			t.Fatal(err)
		}
	})
	a2.SetMaxFrames(1).Run()
	kcMap := generic.NewMap1[runtime.KeyboardControl](a2.Ctx().World())
	kc := kcMap.Get(e2)
	if len(kc.Bindings) != 1 || kc.Bindings[0].DX != 1 || kc.Bindings[0].HoldTicks != 4 {
		t.Fatalf("decoded bindings = %+v", kc.Bindings)
	}
}
