package srcmodel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const assetsSrc = `// Package game wires gameplay to the engine.
package game

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
)

// LoadAssets fills the registries. Comment must survive.
//
//spliti:assets
func LoadAssets(c *app.Ctx) {
	meshes := app.GetResource[render3d.MeshRegistry](c)
	materials := app.GetResource[render3d.MaterialRegistry](c)
	must(meshes.Load("crate", render3d.Cube(1)))
	teapot, err := render3d.LoadOBJ("game/assets/teapot.obj")
	must(err)
	must(meshes.Load("teapot", teapot))
	must(materials.Load("metal", render3d.Material{
		BaseColor: render3d.Color{R: 1, G: 0.78, B: 0.34, A: 1},
		Metallic:  1, Roughness: 0.25,
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
`

func writeAssetsFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "game.go")
	if err := os.WriteFile(path, []byte(assetsSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseAssets(t *testing.T) {
	af, err := ParseAssetsFile(writeAssetsFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if af.meshVar != "meshes" || af.matVar != "materials" {
		t.Fatalf("registry vars: mesh=%q mat=%q", af.meshVar, af.matVar)
	}
	if len(af.Entries) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(af.Entries), af.Entries)
	}
	crate := af.Entry("crate")
	if crate == nil || crate.Kind != AssetMesh || !crate.Procedural {
		t.Fatalf("crate: %+v", crate)
	}
	teapot := af.Entry("teapot")
	if teapot == nil || teapot.Kind != AssetMesh || teapot.File != "game/assets/teapot.obj" {
		t.Fatalf("teapot: %+v", teapot)
	}
	metal := af.Entry("metal")
	if metal == nil || metal.Kind != AssetMaterial || !metal.Editable {
		t.Fatalf("metal: %+v", metal)
	}
}

func TestAddMeshRoundTrip(t *testing.T) {
	path := writeAssetsFile(t)
	af, err := ParseAssetsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := af.AddMesh("barrel", "game/assets/barrel.obj"); err != nil {
		t.Fatal(err)
	}
	if err := af.Save(); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), `meshes.LoadOBJFile("barrel", "game/assets/barrel.obj")`) {
		t.Fatalf("generated line missing:\n%s", out)
	}
	// Re-parse: the new mesh is present and the old ones survived.
	af2, err := ParseAssetsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"crate", "teapot", "metal", "barrel"} {
		if !af2.Has(k) {
			t.Fatalf("asset %q lost after round trip", k)
		}
	}
	if af2.Entry("barrel").File != "game/assets/barrel.obj" {
		t.Fatalf("barrel file = %q", af2.Entry("barrel").File)
	}
}

func TestAddMaterialAndRemove(t *testing.T) {
	path := writeAssetsFile(t)
	af, err := ParseAssetsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := af.AddMaterial("glass", MaterialSpec{R: 0.2, G: 0.4, B: 0.9, A: 1, Roughness: 0.1}); err != nil {
		t.Fatal(err)
	}
	if err := af.Save(); err != nil {
		t.Fatal(err)
	}
	af2, _ := ParseAssetsFile(path)
	if g := af2.Entry("glass"); g == nil || g.Kind != AssetMaterial || !g.Editable {
		t.Fatalf("glass: %+v", g)
	}
	// glass is a single-statement material → removable; teapot (traced var) is not.
	if err := af2.Remove("glass"); err != nil {
		t.Fatalf("remove glass: %v", err)
	}
	if err := af2.Remove("teapot"); err == nil {
		t.Fatal("expected refusal removing a traced-var mesh")
	}
	if err := af2.Save(); err != nil {
		t.Fatal(err)
	}
	af3, _ := ParseAssetsFile(path)
	if af3.Has("glass") {
		t.Fatal("glass still present after remove")
	}
	if !af3.Has("teapot") || !af3.Has("crate") {
		t.Fatal("removing glass dropped other assets")
	}
}
