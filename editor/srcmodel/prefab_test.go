package srcmodel

import (
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/hvuhsg/spliti/plugin/collision"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

func TestRenderPrefab(t *testing.T) {
	comps := []PrefabComponent{
		{TypeName: "collision.Collider3D", Value: reflect.ValueOf(collision.Collider3D{Half: m.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})},
	}
	src, skipped, err := RenderPrefab("SpawnWidget", "teapot", "ceramic", comps)
	if err != nil {
		t.Fatalf("RenderPrefab: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("unexpected skipped components: %v", skipped)
	}
	got := string(src)

	// Must be valid, parseable Go.
	if _, err := parser.ParseFile(token.NewFileSet(), "", got, parser.ParseComments); err != nil {
		t.Fatalf("generated source does not parse: %v\n%s", err, got)
	}

	for _, want := range []string{
		"package entities",
		"//spliti:entity",
		"func SpawnWidget(c *app.Ctx, t render3d.Transform3D) ecs.Entity {",
		`render3d.NewMesh(c, t, "teapot", "ceramic")`,
		"scene.Set(c, e, collision.Collider3D{",
		"return e",
		`"github.com/hvuhsg/spliti/scene"`,
		`"github.com/hvuhsg/spliti/plugin/collision"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated source missing %q\n%s", want, got)
		}
	}
	// The scene.Set must sit before the return.
	if i, j := strings.Index(got, "scene.Set"), strings.LastIndex(got, "return e"); i < 0 || j < 0 || i > j {
		t.Errorf("scene.Set not placed before return\n%s", got)
	}
}

// TestRenderPrefabNoComponents builds a bare mesh prefab.
func TestRenderPrefabNoComponents(t *testing.T) {
	src, skipped, err := RenderPrefab("SpawnPlain", "crate", "metal", nil)
	if err != nil {
		t.Fatalf("RenderPrefab: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("unexpected skipped: %v", skipped)
	}
	got := string(src)
	if _, err := parser.ParseFile(token.NewFileSet(), "", got, parser.ParseComments); err != nil {
		t.Fatalf("does not parse: %v\n%s", err, got)
	}
	if strings.Contains(got, "scene.Set") {
		t.Errorf("bare prefab should have no scene.Set\n%s", got)
	}
	if strings.Contains(got, sceneImportPath) {
		t.Errorf("bare prefab should not import scene\n%s", got)
	}
}
