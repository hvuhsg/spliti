package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/registry"
	"github.com/hvuhsg/spliti/editor/srcmodel"
	"github.com/hvuhsg/spliti/plugin/collision"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/scene"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Health mirrors a game component for command tests.
type Health struct{ Max, Current int }

// Seeker carries an entity reference and a slice, exercising the M4 encode
// paths and the snapshot entity remap.
type Seeker struct {
	Target    ecs.Entity
	Waypoints []m.Vec3
}

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
	registry.Register[Seeker](reg, "Seeker", "components")

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

// TestCmdSetComponentEntityRefAndSlice pushes a component holding an entity
// reference and a slice: the override line must encode the reference as the
// target's spawn variable, and syncInstanceFromModel must decode it back to
// the live handle.
func TestCmdSetComponentEntityRefAndSlice(t *testing.T) {
	c, st := newCmdEditor(t)
	lamp, _ := entityByInstance(c, "lamp")
	crate1, _ := entityByInstance(c, "crate1")
	ti := st.reg.Lookup("Seeker")
	ti.Add(c.World(), lamp)

	want := Seeker{Target: crate1, Waypoints: []m.Vec3{{X: 1}, {Y: 2}}}
	st.push(c, st.newSetComponent("lamp", lamp, ti, Seeker{}, want))
	src := modelSrc(t, st)
	if !strings.Contains(src, "scene.Set(c, lamp, components.Seeker{Target: crate1, Waypoints: []m.Vec3{{X: 1}, {Y: 2}}})") {
		t.Fatalf("entity-ref override line wrong:\n%s", src)
	}
	if !strings.Contains(src, `"github.com/hvuhsg/spliti/plugin/render3d/m"`) {
		t.Fatalf("m import missing:\n%s", src)
	}

	// Decode path: zero the live value, then sync the instance from the model.
	ti.SetValue(c.World(), lamp, Seeker{})
	sp := st.scene().Spawn("lamp")
	if err := syncInstanceFromModel(c, st, sp); err != nil {
		t.Fatal(err)
	}
	got := ti.Value(c.World(), lamp).(Seeker)
	if got.Target != crate1 {
		t.Fatalf("decoded Target = %v, want %v", got.Target, crate1)
	}
	if len(got.Waypoints) != 2 || got.Waypoints[1].Y != 2 {
		t.Fatalf("decoded Waypoints = %+v", got.Waypoints)
	}

	st.undoLast(c)
	if strings.Contains(modelSrc(t, st), "components.Seeker{") {
		t.Fatalf("undo left the Set line:\n%s", modelSrc(t, st))
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

func TestCommitComponentEditMultiSelect(t *testing.T) {
	c, st := newCmdEditor(t)
	crate1, _ := entityByInstance(c, "crate1")
	ground, _ := entityByInstance(c, "ground")
	ti := st.reg.Lookup("Health")
	ti.Add(c.World(), crate1)
	ti.Add(c.World(), ground)

	// Selecting both, an inspector edit on the primary (crate1) propagates the
	// same value to every other selected entity that has the component.
	st.sel = []ecs.Entity{crate1, ground}
	want := Health{Max: 80, Current: 20}
	before := ti.Value(c.World(), crate1) // pre-edit snapshot, as the gesture pins
	ti.SetValue(c.World(), crate1, want)  // widget wrote into the primary's storage
	st.commitComponentEdit(c, crate1, "crate1", ti, before)

	if got := ti.Value(c.World(), crate1).(Health); got != want {
		t.Fatalf("primary live = %+v", got)
	}
	if got := ti.Value(c.World(), ground).(Health); got != want {
		t.Fatalf("multi-edit did not reach ground: %+v", got)
	}
	src := modelSrc(t, st)
	if !strings.Contains(src, "scene.Set(c, crate1, components.Health{Max: 80, Current: 20})") {
		t.Fatalf("crate1 override missing:\n%s", src)
	}
	if !strings.Contains(src, "scene.Set(c, ground, components.Health{Max: 80, Current: 20})") {
		t.Fatalf("ground override missing:\n%s", src)
	}

	// One undo reverts the whole batch.
	st.undoLast(c)
	if got := ti.Value(c.World(), crate1).(Health); got != (Health{}) {
		t.Fatalf("undo primary = %+v", got)
	}
	if got := ti.Value(c.World(), ground).(Health); got != (Health{}) {
		t.Fatalf("undo ground = %+v", got)
	}
	if strings.Contains(modelSrc(t, st), "components.Health{Max") {
		t.Fatalf("undo left an override line:\n%s", modelSrc(t, st))
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

func TestCmdSetComponentSymbolicLayers(t *testing.T) {
	c, st := newCmdEditor(t)
	e, _ := entityByInstance(c, "crate1")
	ti := st.reg.Lookup("Collider3D")
	ti.Add(c.World(), e)

	// Install the named-layer table the way loadLayers does.
	st.symLayers = &srcmodel.Layers{
		Pkg:        "game",
		ImportPath: "testgame/game",
		Names:      []string{"LayerDefault", "LayerPlayer"},
	}
	st.scene().SetLayers(st.symLayers)

	want := collision.Collider3D{Half: m.Vec3{X: 1}, Layer: 1 << 1, Mask: 0b11}
	st.push(c, st.newSetComponent("crate1", e, ti, collision.Collider3D{}, want))
	src := modelSrc(t, st)
	if !strings.Contains(src, "Layer: game.LayerPlayer, Mask: game.LayerDefault | game.LayerPlayer") {
		t.Fatalf("layer bits not symbolic:\n%s", src)
	}
	if !strings.Contains(src, `"testgame/game"`) {
		t.Fatalf("game import missing:\n%s", src)
	}

	// Decode path: zero the live value, sync from the model, get it back.
	ti.SetValue(c.World(), e, collision.Collider3D{})
	if err := syncInstanceFromModel(c, st, st.scene().Spawn("crate1")); err != nil {
		t.Fatal(err)
	}
	if got := ti.Value(c.World(), e).(collision.Collider3D); got != want {
		t.Fatalf("decoded %+v, want %+v", got, want)
	}
}
