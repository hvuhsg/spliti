package srcmodel

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// mirrors the fixture's components.Health for encode/decode tests
type testHealth struct {
	Max, Current int
}

type testVec struct {
	X, Y, Z float32
}

type testNested struct {
	Speed  float32
	Offset testVec
	Name   string
	Live   bool
}

func mustParse(t *testing.T, src string) *Scene {
	t.Helper()
	f, err := ParseSceneSource([]byte(src), "scenes/main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := f.Scene("Main")
	if s == nil {
		t.Fatal("scene Main not found")
	}
	return s
}

func render(t *testing.T, s *Scene) string {
	t.Helper()
	out, err := s.file.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func reparse(t *testing.T, s *Scene) *Scene {
	t.Helper()
	return mustParse(t, render(t, s))
}

func TestRecognizeAttachmentLines(t *testing.T) {
	s := parseTestScene(t)

	crate := s.Spawn("crate1")
	if len(crate.Sets) != 1 || crate.Sets[0].Type != "components.Health" {
		t.Fatalf("crate1 Sets = %+v", crate.Sets)
	}
	if !crate.Sets[0].Editable {
		t.Fatal("literal scene.Set must be editable")
	}
	lamp := s.Spawn("lamp")
	if lamp.ParentLine == nil || lamp.ParentLine.Parent != "crate1" {
		t.Fatalf("lamp parent = %+v", lamp.ParentLine)
	}
	if g := s.Spawn("ground"); len(g.Sets) != 0 || g.ParentLine != nil {
		t.Fatalf("ground gained attachments: %+v", g)
	}
}

func TestEnsureVarPromotesAssignForms(t *testing.T) {
	s := parseTestScene(t)
	// "ground" is the `_ =` form.
	name, err := s.EnsureVar("ground")
	if err != nil {
		t.Fatal(err)
	}
	if name != "ground" {
		t.Fatalf("var = %q", name)
	}
	src := render(t, s)
	if !strings.Contains(src, `ground := scene.Spawn(c, "ground"`) {
		t.Fatalf("promotion missing:\n%s", src)
	}
	s2 := reparse(t, s)
	if s2.Spawn("ground").Var != "ground" {
		t.Fatal("promoted var not recognized on re-parse")
	}
	// Idempotent on an already-bound spawn.
	if name, err := s2.EnsureVar("crate1"); err != nil || name != "crate1" {
		t.Fatalf("EnsureVar(crate1) = %q, %v", name, err)
	}
}

func TestSetComponentValueUpsertsLine(t *testing.T) {
	s := parseTestScene(t)

	// Update the existing override line.
	v := reflect.ValueOf(testHealth{Max: 80, Current: 20})
	if err := s.SetComponentValue("crate1", "components.Health", v); err != nil {
		t.Fatal(err)
	}
	src := render(t, s)
	if !strings.Contains(src, `scene.Set(c, crate1, components.Health{Max: 80, Current: 20})`) {
		t.Fatalf("override not rewritten:\n%s", src)
	}
	if strings.Count(src, "components.Health{") != 1 {
		t.Fatalf("duplicate Set lines:\n%s", src)
	}

	// Insert a new line for an instance without a var (promotes `_ =`).
	v2 := reflect.ValueOf(testHealth{Max: 10})
	if err := s.SetComponentValue("ground", "components.Health", v2); err != nil {
		t.Fatal(err)
	}
	src = render(t, s)
	if !strings.Contains(src, `scene.Set(c, ground, components.Health{Max: 10})`) {
		t.Fatalf("new Set line missing:\n%s", src)
	}
	// Zero fields are omitted.
	if strings.Contains(src, "Current: 0") {
		t.Fatalf("zero field emitted:\n%s", src)
	}

	s2 := reparse(t, s)
	if s2.Spawn("ground").Set("components.Health") == nil {
		t.Fatal("inserted Set line not recognized on re-parse")
	}

	// Decode back.
	var got testHealth
	rv := reflect.ValueOf(&got).Elem()
	if err := ApplyLit(s2.Spawn("crate1").Set("components.Health").Lit(), rv); err != nil {
		t.Fatal(err)
	}
	if got != (testHealth{Max: 80, Current: 20}) {
		t.Fatalf("decoded %+v", got)
	}
}

func TestSetComponentValueNestedStructAddsImport(t *testing.T) {
	s := parseTestScene(t)
	v := reflect.ValueOf(testNested{
		Speed:  1.5,
		Offset: testVec{X: 1, Z: -2},
		Name:   "boost",
		Live:   true,
	})
	if err := s.SetComponentValue("crate1", "components.Boost", v); err != nil {
		t.Fatal(err)
	}
	src := render(t, s)
	if !strings.Contains(src, `components.Boost{Speed: 1.5, Offset: srcmodel.testVec{X: 1, Z: -2}, Name: "boost", Live: true}`) {
		t.Fatalf("nested literal wrong:\n%s", src)
	}
	// The nested type's package import was added.
	if !strings.Contains(src, `"github.com/hvuhsg/spliti/editor/srcmodel"`) {
		t.Fatalf("nested import missing:\n%s", src)
	}
}

func TestRemoveSetAndAddRemoveLines(t *testing.T) {
	s := parseTestScene(t)
	had, err := s.RemoveSet("crate1", "components.Health")
	if err != nil || !had {
		t.Fatalf("RemoveSet = %v, %v", had, err)
	}
	src := render(t, s)
	if strings.Contains(src, "components.Health{Max: 50") {
		t.Fatalf("Set line survived:\n%s", src)
	}

	if err := s.AddRemove("crate1", "components.Spinner"); err != nil {
		t.Fatal(err)
	}
	// Idempotent.
	if err := s.AddRemove("crate1", "components.Spinner"); err != nil {
		t.Fatal(err)
	}
	src = render(t, s)
	if strings.Count(src, "scene.Remove[components.Spinner](c, crate1)") != 1 {
		t.Fatalf("Remove line wrong:\n%s", src)
	}
	s2 := reparse(t, s)
	if s2.Spawn("crate1").Removed("components.Spinner") == nil {
		t.Fatal("Remove line not recognized on re-parse")
	}
	had, err = s2.DeleteRemove("crate1", "components.Spinner")
	if err != nil || !had {
		t.Fatalf("DeleteRemove = %v, %v", had, err)
	}
	if strings.Contains(render(t, s2), "scene.Remove[components.Spinner]") {
		t.Fatal("Remove line survived deletion")
	}
}

func TestSetParentUpsertsAndUnparents(t *testing.T) {
	s := parseTestScene(t)

	// Re-target lamp's existing parent line to ground (promoting ground's var).
	if err := s.SetParent("lamp", "ground"); err != nil {
		t.Fatal(err)
	}
	src := render(t, s)
	if !strings.Contains(src, "scene.Parent(c, lamp, ground)") {
		t.Fatalf("parent line not retargeted:\n%s", src)
	}
	if strings.Contains(src, "scene.Parent(c, lamp, crate1)") {
		t.Fatalf("old parent line survived:\n%s", src)
	}
	s2 := reparse(t, s)
	if pl := s2.Spawn("lamp").ParentLine; pl == nil || pl.Parent != "ground" {
		t.Fatalf("re-parse parent = %+v", pl)
	}

	// New parent line between previously unrelated instances.
	if err := s2.SetParent("quat", "weird"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(render(t, s2), "scene.Parent(c, quat, weird)") {
		t.Fatal("new parent line missing")
	}

	// Unparent removes the line.
	if err := s2.SetParent("lamp", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(render(t, s2), "scene.Parent(c, lamp, ground)") {
		t.Fatal("unparent left the line")
	}
}

func TestRenameChangesOnlyTheLiteral(t *testing.T) {
	s := parseTestScene(t)
	if err := s.Rename("crate1", "hero-crate"); err != nil {
		t.Fatal(err)
	}
	src := render(t, s)
	if !strings.Contains(src, `scene.Spawn(c, "hero-crate"`) {
		t.Fatalf("rename missing:\n%s", src)
	}
	// Var stays; attachment lines still bind through it.
	s2 := reparse(t, s)
	sp := s2.Spawn("hero-crate")
	if sp == nil || sp.Var != "crate1" || len(sp.Sets) != 1 {
		t.Fatalf("renamed spawn = %+v", sp)
	}
	if s2.Spawn("lamp").ParentLine.Parent != "hero-crate" {
		t.Fatal("parent binding lost after rename")
	}
	// Duplicate and unknown names are rejected.
	if err := s2.Rename("ground", "hero-crate"); err == nil {
		t.Fatal("duplicate rename accepted")
	}
	if err := s2.Rename("nope", "x"); err == nil {
		t.Fatal("unknown rename accepted")
	}
}

func TestRemoveSpawnDeletesAttachmentsAndRestores(t *testing.T) {
	s := parseTestScene(t)
	before := render(t, s)

	rem, err := s.RemoveSpawn("lamp")
	if err != nil {
		t.Fatal(err)
	}
	src := render(t, s)
	for _, gone := range []string{`"lamp"`, "scene.Parent(c, lamp, crate1)"} {
		if strings.Contains(src, gone) {
			t.Fatalf("deletion left %q:\n%s", gone, src)
		}
	}
	// crate1 and its Set line survive.
	if !strings.Contains(src, "components.Health{Max: 50, Current: 50}") {
		t.Fatalf("unrelated line lost:\n%s", src)
	}

	if err := s.RestoreSpawn(rem); err != nil {
		t.Fatal(err)
	}
	if got := render(t, s); got != before {
		t.Fatalf("restore not byte-identical:\n--- want\n%s\n--- got\n%s", before, got)
	}
}

func TestRemoveSpawnDeletesChildParentLines(t *testing.T) {
	s := parseTestScene(t)
	// Deleting crate1: lamp's parent line references it and must go too; the
	// Set line on crate1 must go; lamp's spawn itself survives.
	rem, err := s.RemoveSpawn("crate1")
	if err != nil {
		t.Fatal(err)
	}
	src := render(t, s)
	for _, gone := range []string{`"crate1"`, "scene.Parent", "components.Health{"} {
		if strings.Contains(src, gone) {
			t.Fatalf("deletion left %q:\n%s", gone, src)
		}
	}
	if !strings.Contains(src, `"lamp"`) {
		t.Fatalf("lamp lost:\n%s", src)
	}
	if len(rem.stmts) != 3 {
		t.Fatalf("captured %d stmts, want 3", len(rem.stmts))
	}
}

func TestSaveRemovesOrphanedImports(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(sceneSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := ParseSceneFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := f.Scene("Main")

	// A nested-struct override pulls in the srcmodel import...
	v := reflect.ValueOf(testNested{Offset: testVec{X: 1}})
	if err := s.SetComponentValue("crate1", "components.Boost", v); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), `"github.com/hvuhsg/spliti/editor/srcmodel"`) {
		t.Fatalf("import not added:\n%s", out)
	}

	// ...and removing the line must take the now-unused import with it.
	if _, err := s.RemoveSet("crate1", "components.Boost"); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	out, _ = os.ReadFile(path)
	if strings.Contains(string(out), `"github.com/hvuhsg/spliti/editor/srcmodel"`) {
		t.Fatalf("orphaned import survived save:\n%s", out)
	}
	// Imports that are still used must survive.
	for _, keep := range []string{`"demo/game/entities"`, `"github.com/hvuhsg/spliti/scene"`, `"github.com/hvuhsg/spliti/plugin/render3d"`} {
		if !strings.Contains(string(out), keep) {
			t.Fatalf("live import %s pruned:\n%s", keep, out)
		}
	}
}

func TestFromTransform3DRoundsFloatNoise(t *testing.T) {
	v := render3d.Transform3D{
		Translation: m.Vec3{X: -5.5311716e-08, Y: 0.056345563, Z: 5.152288},
		Rotation:    m.IdentityQuat(),
		Scale:       m.Vec3{X: 2.1975043, Y: 1, Z: 1},
	}
	tr := FromTransform3D(v)
	if *tr.At != [3]float32{0, 0.056, 5.152} {
		t.Fatalf("At = %v", *tr.At)
	}
	if *tr.Scaled != [3]float32{2.198, 1, 1} {
		t.Fatalf("Scaled = %v", *tr.Scaled)
	}
	// Noise-only translation rounds to zero and emits no call at all.
	v2 := render3d.Transform3D{
		Translation: m.Vec3{X: 1e-9, Y: -3e-8},
		Rotation:    m.IdentityQuat(),
		Scale:       m.Vec3{X: 1, Y: 1, Z: 1},
	}
	if tr2 := FromTransform3D(v2); tr2.At != nil || tr2.Scaled != nil {
		t.Fatalf("noise emitted calls: %+v", tr2)
	}
}

func TestRemoveSpawnRefusesWhenVarUsedByHandWrittenCode(t *testing.T) {
	src := `package scenes

import (
	"demo/game/entities"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"
)

//spliti:scene Main
func Main(c *app.Ctx) {
	crate := scene.Spawn(c, "crate", entities.SpawnCrate(c, render3d.XForm()))
	doSomething(crate)
}
`
	s := mustParse(t, src)
	if _, err := s.RemoveSpawn("crate"); err == nil {
		t.Fatal("expected refusal: var referenced by opaque statement")
	}
}

// TestRestoreSpawnReEnsuresImports reproduces the delete -> save -> undo
// corruption: a save while the only components-using spawn is deleted prunes
// the import, and the undo's RestoreSpawn must put it back — otherwise the
// editor writes a scene file that references a package it no longer imports.
func TestRestoreSpawnReEnsuresImports(t *testing.T) {
	s := parseTestScene(t)
	// crate1 holds the scene's components.* Set lines in the test scene; once
	// every other components user is gone, deleting it leaves the components
	// import unused. The test scene's lamp/weird/quat don't use components.
	rem, err := s.RemoveSpawn("crate1")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the debounced save while deleted: prune runs on every Save.
	pruneUnusedImports(s.file.file)
	if src := render(t, s); strings.Contains(src, `"demo/game/components"`) &&
		strings.Contains(src, "components.") {
		t.Skip("test scene still uses components elsewhere; pick another fixture")
	}

	if err := s.RestoreSpawn(rem); err != nil {
		t.Fatal(err)
	}
	src := render(t, s)
	if strings.Contains(src, "components.") && !strings.Contains(src, `"demo/game/components"`) {
		t.Fatalf("restored lines reference components but the import is gone:\n%s", src)
	}
}
