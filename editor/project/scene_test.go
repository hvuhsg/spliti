package project_test

import (
	"path/filepath"
	"testing"

	"github.com/hvuhsg/spliti/editor/project"
)

func TestScene_LoadParsesEntitiesAndComponents(t *testing.T) {
	sf, err := project.LoadSceneFile("testdata/main.scene")
	if err != nil {
		t.Fatal(err)
	}
	if sf.Schema != 1 {
		t.Fatalf("schema = %d, want 1", sf.Schema)
	}
	if sf.Name != "main" {
		t.Fatalf("name = %q, want main", sf.Name)
	}
	if len(sf.Entities) != 2 {
		t.Fatalf("entities = %d, want 2", len(sf.Entities))
	}
	first := sf.Entities[0]
	if first.Name != "left_paddle" {
		t.Fatalf("first entity name = %q, want left_paddle", first.Name)
	}
	pos, ok := first.Components["position"].(map[string]any)
	if !ok {
		t.Fatalf("position component missing or wrong type: %#v", first.Components["position"])
	}
	if x, _ := pos["x"].(int64); x != 2 {
		t.Fatalf("position.x = %v, want 2", pos["x"])
	}
}

func TestScene_RoundTripPreservesData(t *testing.T) {
	dir := t.TempDir()
	src := &project.SceneFile{
		Schema: 1,
		Name:   "lvl1",
		Entities: []project.SceneEntity{
			{
				Name: "ball",
				Components: map[string]any{
					"position": map[string]any{"x": int64(10), "y": int64(5)},
					"velocity": map[string]any{"dx": int64(1), "dy": int64(-1)},
					"tag":      map[string]any{"name": "ball"},
				},
			},
			{
				Name: "wall",
				Components: map[string]any{
					"position": map[string]any{"x": int64(0), "y": int64(0)},
					"bounds":   map[string]any{"w": int64(20), "h": int64(1)},
				},
			},
		},
	}
	path := filepath.Join(dir, "lvl1.scene")
	if err := project.SaveSceneFile(path, src); err != nil {
		t.Fatal(err)
	}
	got, err := project.LoadSceneFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != src.Name || got.Schema != src.Schema {
		t.Fatalf("header round-trip mismatch: %+v", got)
	}
	if len(got.Entities) != len(src.Entities) {
		t.Fatalf("entity count = %d, want %d", len(got.Entities), len(src.Entities))
	}
	for i, e := range src.Entities {
		ge := got.Entities[i]
		if ge.Name != e.Name {
			t.Fatalf("entity %d name = %q, want %q", i, ge.Name, e.Name)
		}
		for cname, cval := range e.Components {
			gv, ok := ge.Components[cname]
			if !ok {
				t.Fatalf("entity %s missing component %s", e.Name, cname)
			}
			gMap, _ := gv.(map[string]any)
			eMap, _ := cval.(map[string]any)
			for k, v := range eMap {
				if gMap[k] != v {
					t.Fatalf("entity %s.%s.%s = %v, want %v", e.Name, cname, k, gMap[k], v)
				}
			}
		}
	}
}

func TestScene_DeterministicOutput(t *testing.T) {
	// Two encodes of the same scene must produce byte-identical output —
	// otherwise scene-file diffs flap on every save.
	src := &project.SceneFile{
		Schema: 1, Name: "x",
		Entities: []project.SceneEntity{{
			Name: "e",
			Components: map[string]any{
				"z_late":  map[string]any{"a": int64(1)},
				"a_early": map[string]any{"b": int64(2)},
			},
		}},
	}
	a, err := project.EncodeScene(src)
	if err != nil {
		t.Fatal(err)
	}
	b, err := project.EncodeScene(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("encodings differ:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}
