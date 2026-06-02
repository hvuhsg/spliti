package project_test

import (
	"testing"

	"github.com/hvuhsg/spliti/editor/project"
	"github.com/hvuhsg/spliti/editor/schema"
)

func TestCustomComponents_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := &schema.CustomComponentsFile{
		Components: []schema.CustomComponentSchema{
			{
				Name: "Health",
				Fields: []schema.CustomFieldSchema{
					{Name: "Value", Kind: "int"},
					{Name: "Max", Kind: "int"},
				},
			},
			{
				Name: "Spawner",
				Fields: []schema.CustomFieldSchema{
					{Name: "IntervalTicks", Kind: "int"},
					{Name: "SpriteRef", Kind: "string"},
				},
			},
		},
	}
	if err := project.SaveCustomComponents(dir, src); err != nil {
		t.Fatal(err)
	}
	got, err := project.LoadCustomComponents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Components) != 2 {
		t.Fatalf("count = %d, want 2", len(got.Components))
	}
	if got.Components[0].Name != "Health" || len(got.Components[0].Fields) != 2 {
		t.Fatalf("Health: %+v", got.Components[0])
	}
	if got.Components[1].Name != "Spawner" || got.Components[1].Fields[1].Kind != "string" {
		t.Fatalf("Spawner: %+v", got.Components[1])
	}
}

func TestCustomComponents_MissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := project.LoadCustomComponents(dir)
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(got.Components) != 0 {
		t.Fatalf("expected empty, got %d", len(got.Components))
	}
}
