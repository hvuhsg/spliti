package components_test

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/components"
	"github.com/hvuhsg/spliti/editor/schema"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
)

// TestCustom_RegisterAddEncodeDecode exercises the full lifecycle of a
// custom component type:
//   - Register the schema
//   - Add it to an entity (through the synthetic ComponentDesc)
//   - Mutate a field via Set
//   - Encode the entity and verify the output matches schema
//   - Decode into a fresh entity and verify values transfer
func TestCustom_RegisterAddEncodeDecode(t *testing.T) {
	components.Reset()
	components.RegisterBuiltins()
	components.RegisterCustom(&schema.CustomComponentsFile{
		Components: []schema.CustomComponentSchema{{
			Name: "Health",
			Fields: []schema.CustomFieldSchema{
				{Name: "Value", Kind: "int"},
				{Name: "Max", Kind: "int"},
				{Name: "Name", Kind: "string"},
			},
		}},
	})

	a := app.New()
	var e1 ecs.Entity
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		e1 = c.World().NewEntity()
		desc := components.ByName("Health")
		if desc == nil {
			t.Fatal("Health not registered")
		}
		if desc.Has(c.World(), e1) {
			t.Fatal("brand-new entity should not have Health")
		}
		desc.Add(c, e1)
		if !desc.Has(c.World(), e1) {
			t.Fatal("Add did not produce Has()")
		}
		// Mutate fields.
		for _, f := range desc.Fields {
			switch f.Name {
			case "Value":
				f.Set(c, e1, 42)
			case "Max":
				f.Set(c, e1, 100)
			case "Name":
				f.Set(c, e1, "hero")
			}
		}
	})
	a.SetMaxFrames(1).Run()

	desc := components.ByName("Health")
	w := a.Ctx().World()
	enc, ok := desc.Encode(w, e1)
	if !ok {
		t.Fatal("encode false")
	}
	if enc["Value"].(int64) != 42 || enc["Max"].(int64) != 100 || enc["Name"].(string) != "hero" {
		t.Fatalf("encoded mismatch: %+v", enc)
	}

	// Decode into a fresh entity.
	a2 := app.New()
	var e2 ecs.Entity
	a2.AddSystems(schedule.Startup, func(c *app.Ctx) {
		e2 = c.World().NewEntity()
		if err := desc.Decode(c, e2, enc); err != nil {
			t.Fatalf("decode: %v", err)
		}
	})
	a2.SetMaxFrames(1).Run()

	desc2 := components.ByName("Health")
	w2 := a2.Ctx().World()
	if !desc2.Has(w2, e2) {
		t.Fatal("post-decode: missing Has")
	}
	out, ok := desc2.Encode(w2, e2)
	if !ok {
		t.Fatal("re-encode false")
	}
	if out["Value"].(int64) != 42 || out["Max"].(int64) != 100 || out["Name"].(string) != "hero" {
		t.Fatalf("re-encoded mismatch: %+v", out)
	}
}

func TestCustom_RegisterTwiceUnregistersOld(t *testing.T) {
	components.Reset()
	components.RegisterBuiltins()
	components.RegisterCustom(&schema.CustomComponentsFile{
		Components: []schema.CustomComponentSchema{
			{Name: "Old", Fields: []schema.CustomFieldSchema{{Name: "X", Kind: "int"}}},
		},
	})
	if components.ByName("Old") == nil {
		t.Fatal("Old not registered")
	}
	components.RegisterCustom(&schema.CustomComponentsFile{
		Components: []schema.CustomComponentSchema{
			{Name: "New", Fields: []schema.CustomFieldSchema{{Name: "Y", Kind: "string"}}},
		},
	})
	if components.ByName("Old") != nil {
		t.Fatal("Old should have been unregistered")
	}
	if components.ByName("New") == nil {
		t.Fatal("New not registered")
	}
}
