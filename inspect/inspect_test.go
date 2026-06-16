package inspect_test

import (
	"encoding/json"
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/inspect"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

type Position struct{ X, Y float64 }
type Health struct{ HP int }
type Target struct{ Of ecs.Entity } // entity-reference field

type Score struct{ Points int } // a resource with exported state

func TestWorldDumpComponentsAndRefs(t *testing.T) {
	a := app.New()
	app.InsertResource(a, &Score{Points: 3})

	var dump inspect.Dump
	var e1id uint32
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		w := c.World()
		m := generic.NewMap2[Position, Health](w)
		e1 := m.NewWith(&Position{X: 5, Y: -2}, &Health{HP: 10})
		e1id = e1.ID()
		e2 := w.NewEntity()
		mt := generic.NewMap1[Target](w)
		mt.Assign(e2, &Target{Of: e1})
		dump = inspect.World(c)
	}).SetMaxFrames(1).Run()

	if len(dump.Entities) != 2 {
		t.Fatalf("entities=%d, want 2", len(dump.Entities))
	}

	var posRaw, targetRaw json.RawMessage
	for _, e := range dump.Entities {
		if r, ok := e.Components["inspect_test.Position"]; ok {
			posRaw = r
		}
		if r, ok := e.Components["inspect_test.Target"]; ok {
			targetRaw = r
		}
	}

	if posRaw == nil {
		t.Fatal("no entity carried a Position component")
	}
	var pos Position
	if err := json.Unmarshal(posRaw, &pos); err != nil {
		t.Fatalf("unmarshal Position: %v", err)
	}
	if pos.X != 5 || pos.Y != -2 {
		t.Fatalf("Position=%+v, want {5 -2}", pos)
	}

	// Entity-reference field round-trips through arche's [id, gen] form.
	if targetRaw == nil {
		t.Fatal("no entity carried a Target component")
	}
	var target Target
	if err := json.Unmarshal(targetRaw, &target); err != nil {
		t.Fatalf("unmarshal Target: %v", err)
	}
	if target.Of.ID() != e1id {
		t.Fatalf("Target.Of.ID()=%d, want %d (the referenced entity)", target.Of.ID(), e1id)
	}
}

func TestResourceDumpKeepsGameStateSkipsEmpty(t *testing.T) {
	a := app.New()
	app.InsertResource(a, &Score{Points: 7})
	// A resource with no exported state marshals to "{}" and must be skipped.
	type internalState struct{ hidden int }
	app.InsertResource(a, &internalState{hidden: 1})

	var dump inspect.Dump
	a.AddSystems(schedule.Startup, func(c *app.Ctx) { dump = inspect.World(c) }).SetMaxFrames(1).Run()

	raw, ok := dump.Resources["inspect_test.Score"]
	if !ok {
		t.Fatalf("Score resource missing; got resources %v", dump.Resources)
	}
	var sc Score
	if err := json.Unmarshal(raw, &sc); err != nil || sc.Points != 7 {
		t.Fatalf("Score=%+v err=%v, want Points=7", sc, err)
	}
	if _, ok := dump.Resources["inspect_test.internalState"]; ok {
		t.Fatal("empty-state resource should have been skipped")
	}
}

func TestWorldJSONIsValid(t *testing.T) {
	a := app.New()
	var raw []byte
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		mh := generic.NewMap1[Health](c.World())
		mh.NewWith(&Health{HP: 1})
		raw = inspect.WorldJSON(c)
	}).SetMaxFrames(1).Run()

	var out inspect.Dump
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("WorldJSON produced invalid JSON: %v\n%s", err, raw)
	}
	if len(out.Entities) != 1 {
		t.Fatalf("entities=%d, want 1", len(out.Entities))
	}
}
