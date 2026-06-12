package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/registry"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Health mirrors a game component for command tests.
type Health struct{ Max, Current int }

const cmdScene = `package scenes

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"

	"demo/game/entities"
)

//spliti:scene Main
func Main(c *app.Ctx) {
	_ = scene.Spawn(c, "ground", entities.SpawnCrate(c, render3d.XForm()))
	crate1 := scene.Spawn(c, "crate1", entities.SpawnCrate(c, render3d.XForm().At(-1.4, 0.5, 0)))
	lamp := scene.Spawn(c, "lamp", entities.SpawnCrate(c, render3d.XForm().At(0, 1.8, 0)))
	scene.Parent(c, lamp, crate1)
}
`

// newCmdEditor builds a headless editor world that mirrors cmdScene: live
// entities for each spawn line, a Health registration, and a SpawnCrate
// prefab.
func newCmdEditor(t *testing.T) (*app.Ctx, *state) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(cmdScene), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := registry.New()
	registry.Builtin(reg)
	registry.Register[Health](reg, "Health", "components")

	prefab := func(c *app.Ctx, tr render3d.Transform3D) ecs.Entity {
		mp := generic.NewMap2[render3d.Transform3D, render3d.GlobalTransform](c.World())
		return mp.NewWith(&tr, &render3d.GlobalTransform{})
	}

	a := app.New()
	st := newState(Plugin{
		ProjectRoot: dir, SceneFile: "main.go", Scene: "Main",
		Registry: reg,
		Prefabs:  map[string]PrefabFunc{"entities.SpawnCrate": prefab},
	})
	app.InsertResource(a, st)
	c := a.Ctx()

	ground := scene.Spawn(c, "ground", prefab(c, render3d.XForm()))
	crate1 := scene.Spawn(c, "crate1", prefab(c, render3d.XForm().At(-1.4, 0.5, 0)))
	lamp := scene.Spawn(c, "lamp", prefab(c, render3d.XForm().At(0, 1.8, 0)))
	scene.Parent(c, lamp, crate1)
	_, _, _ = ground, crate1, lamp

	st.loadSceneSource()
	if st.srcErr != nil {
		t.Fatalf("loadSceneSource: %v", st.srcErr)
	}
	return c, st
}

func modelSrc(t *testing.T, st *state) string {
	t.Helper()
	out, err := st.src.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestCmdTransformUndoRedo(t *testing.T) {
	c, st := newCmdEditor(t)
	e, _ := entityByInstance(c, "crate1")
	tm := generic.NewMap[render3d.Transform3D](c.World())
	before := *tm.Get(e)

	st.push(c, &cmdTransform{
		instance: "crate1", entity: e,
		before: before,
		after:  render3d.XForm().At(3, 0.5, 1).EulerDeg(0, 90, 0),
	})
	if !strings.Contains(modelSrc(t, st), "At(3, 0.5, 1).EulerDeg(0, 90, 0)") {
		t.Fatal("transform not written to model")
	}
	st.undoLast(c)
	if got := *tm.Get(e); got != before {
		t.Fatalf("undo: live = %+v", got)
	}
	if !strings.Contains(modelSrc(t, st), "At(-1.4, 0.5, 0)") {
		t.Fatal("undo: model not restored")
	}
	st.redo(c)
	if got := tm.Get(e).Translation.X; got != 3 {
		t.Fatalf("redo: X = %v", got)
	}
}

func TestCmdSetComponentWritesOverrideLine(t *testing.T) {
	c, st := newCmdEditor(t)
	e, _ := entityByInstance(c, "crate1")
	ti := st.reg.Lookup("Health")
	ti.Add(c.World(), e)

	st.push(c, st.newSetComponent("crate1", e, ti, Health{}, Health{Max: 80, Current: 20}))
	if !strings.Contains(modelSrc(t, st), "scene.Set(c, crate1, components.Health{Max: 80, Current: 20})") {
		t.Fatalf("override line missing:\n%s", modelSrc(t, st))
	}
	if got := ti.Value(c.World(), e).(Health); got.Max != 80 {
		t.Fatalf("live = %+v", got)
	}
	st.undoLast(c)
	if strings.Contains(modelSrc(t, st), "components.Health{") {
		t.Fatal("undo left the override line")
	}
	if got := ti.Value(c.World(), e).(Health); got != (Health{}) {
		t.Fatalf("undo live = %+v", got)
	}
}

func TestCmdAddRemoveComponent(t *testing.T) {
	c, st := newCmdEditor(t)
	e, _ := entityByInstance(c, "ground")
	ti := st.reg.Lookup("Health")

	st.push(c, &cmdAddComponent{instance: "ground", entity: e, typeName: "Health"})
	if !ti.Has(c.World(), e) {
		t.Fatal("component not added")
	}
	if !strings.Contains(modelSrc(t, st), "scene.Set(c, ground, components.Health{})") {
		t.Fatalf("zero Set line missing:\n%s", modelSrc(t, st))
	}

	st.push(c, &cmdRemoveComponent{instance: "ground", entity: e, typeName: "Health"})
	if ti.Has(c.World(), e) {
		t.Fatal("component not removed")
	}
	src := modelSrc(t, st)
	if strings.Contains(src, "scene.Set(c, ground, components.Health") {
		t.Fatal("Set line survived removal")
	}
	if !strings.Contains(src, "scene.Remove[components.Health](c, ground)") {
		t.Fatalf("Remove line missing:\n%s", src)
	}

	st.undoLast(c) // undo remove
	if !ti.Has(c.World(), e) {
		t.Fatal("undo remove: component missing")
	}
	st.undoLast(c) // undo add
	if ti.Has(c.World(), e) {
		t.Fatal("undo add: component still present")
	}
	if strings.Contains(modelSrc(t, st), "components.Health") {
		t.Fatalf("undo add: source still mentions Health:\n%s", modelSrc(t, st))
	}
}

func TestCmdSpawnAndUndo(t *testing.T) {
	c, st := newCmdEditor(t)
	st.push(c, &cmdSpawn{instance: "crate2", prefab: "entities.SpawnCrate",
		t: render3d.XForm().At(1, 0, 2)})
	if _, ok := entityByInstance(c, "crate2"); !ok {
		t.Fatal("live entity missing")
	}
	if !strings.Contains(modelSrc(t, st), `scene.Spawn(c, "crate2", entities.SpawnCrate(c, render3d.XForm().At(1, 0, 2)))`) {
		t.Fatalf("spawn line missing:\n%s", modelSrc(t, st))
	}
	st.undoLast(c)
	if _, ok := entityByInstance(c, "crate2"); ok {
		t.Fatal("undo left the live entity")
	}
	if strings.Contains(modelSrc(t, st), "crate2") {
		t.Fatal("undo left the spawn line")
	}
}

func TestCmdDeleteSubtreeAndUndo(t *testing.T) {
	c, st := newCmdEditor(t)
	before := modelSrc(t, st)

	st.push(c, &cmdDelete{root: "crate1"})
	if _, ok := entityByInstance(c, "crate1"); ok {
		t.Fatal("crate1 still alive")
	}
	if _, ok := entityByInstance(c, "lamp"); ok {
		t.Fatal("child lamp still alive")
	}
	src := modelSrc(t, st)
	if strings.Contains(src, "crate1") || strings.Contains(src, "lamp") {
		t.Fatalf("source still mentions deleted subtree:\n%s", src)
	}

	st.undoLast(c)
	if got := modelSrc(t, st); got != before {
		t.Fatalf("undo: source differs:\n--- want\n%s\n--- got\n%s", before, got)
	}
	crate, ok1 := entityByInstance(c, "crate1")
	lamp, ok2 := entityByInstance(c, "lamp")
	if !ok1 || !ok2 {
		t.Fatal("undo did not respawn the subtree")
	}
	pm := generic.NewMap[render3d.Parent](c.World())
	if !pm.Has(lamp) || pm.Get(lamp).Entity != crate {
		t.Fatal("undo did not relink lamp under crate1")
	}
}

func TestCmdRenameAndReparent(t *testing.T) {
	c, st := newCmdEditor(t)

	st.push(c, &cmdRename{old: "crate1", new: "hero"})
	if _, ok := entityByInstance(c, "hero"); !ok {
		t.Fatal("live rename missing")
	}
	if !strings.Contains(modelSrc(t, st), `"hero"`) {
		t.Fatal("source rename missing")
	}
	st.undoLast(c)
	if _, ok := entityByInstance(c, "crate1"); !ok {
		t.Fatal("undo rename failed")
	}

	// Reparent lamp from crate1 to ground.
	lamp, _ := entityByInstance(c, "lamp")
	tm := generic.NewMap[render3d.Transform3D](c.World())
	oldLocal := *tm.Get(lamp)
	st.push(c, &cmdReparent{
		instance:  "lamp",
		oldParent: "crate1", newParent: "ground",
		oldLocal: oldLocal, newLocal: oldLocal,
	})
	pm := generic.NewMap[render3d.Parent](c.World())
	ground, _ := entityByInstance(c, "ground")
	if pm.Get(lamp).Entity != ground {
		t.Fatal("live reparent missing")
	}
	if !strings.Contains(modelSrc(t, st), "scene.Parent(c, lamp, ground)") {
		t.Fatalf("source reparent missing:\n%s", modelSrc(t, st))
	}
	st.undoLast(c)
	crate, _ := entityByInstance(c, "crate1")
	if pm.Get(lamp).Entity != crate {
		t.Fatal("undo reparent failed")
	}
}

func TestCmdDuplicateCopiesOverrides(t *testing.T) {
	c, st := newCmdEditor(t)
	// Give crate1 an override to copy.
	e, _ := entityByInstance(c, "crate1")
	ti := st.reg.Lookup("Health")
	ti.Add(c.World(), e)
	st.push(c, st.newSetComponent("crate1", e, ti, Health{}, Health{Max: 50, Current: 50}))

	st.push(c, &cmdDuplicate{src: "crate1", instance: "crate1copy"})
	dup, ok := entityByInstance(c, "crate1copy")
	if !ok {
		t.Fatal("duplicate not spawned")
	}
	if got := ti.Value(c.World(), dup).(Health); got.Max != 50 {
		t.Fatalf("override not copied live: %+v", got)
	}
	if !strings.Contains(modelSrc(t, st), `scene.Set(c, crate1copy, components.Health{Max: 50, Current: 50})`) {
		t.Fatalf("override not copied in source:\n%s", modelSrc(t, st))
	}
	st.undoLast(c)
	if _, ok := entityByInstance(c, "crate1copy"); ok {
		t.Fatal("undo left the duplicate")
	}
}

func TestReloadSyncsStructure(t *testing.T) {
	c, st := newCmdEditor(t)

	// External edit: drop lamp, add crate9.
	edited := strings.ReplaceAll(cmdScene,
		"	lamp := scene.Spawn(c, \"lamp\", entities.SpawnCrate(c, render3d.XForm().At(0, 1.8, 0)))\n	scene.Parent(c, lamp, crate1)\n",
		"	_ = scene.Spawn(c, \"crate9\", entities.SpawnCrate(c, render3d.XForm().At(7, 0, 7)))\n")
	if err := os.WriteFile(st.sceneFilePath(), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	reloadScene(c, st)
	if st.srcErr != nil {
		t.Fatalf("reload: %v", st.srcErr)
	}
	if _, ok := entityByInstance(c, "lamp"); ok {
		t.Fatal("removed instance still alive")
	}
	e, ok := entityByInstance(c, "crate9")
	if !ok {
		t.Fatal("added instance not spawned")
	}
	tm := generic.NewMap[render3d.Transform3D](c.World())
	if tm.Get(e).Translation.X != 7 {
		t.Fatalf("spawned transform wrong: %+v", tm.Get(e))
	}
}
