package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

const testScene = `package scenes

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"

	"demo/game/entities"
)

//spliti:scene Main
func Main(c *app.Ctx) {
	_ = scene.Spawn(c, "crate1", entities.SpawnCrate(c, render3d.XForm().At(-1.4, 0.5, 0)))
}
`

// newTestEditor builds an app with the editor state resource pointed at a
// scene file on disk and one live named entity, without any GPU/ImGui — only
// the writeback/reload halves are exercised.
func newTestEditor(t *testing.T) (*app.Ctx, *state, ecs.Entity, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(testScene), 0o644); err != nil {
		t.Fatal(err)
	}

	a := app.New()
	st := newState(Plugin{ProjectRoot: dir, SceneFile: "main.go", Scene: "Main"})
	app.InsertResource(a, st)
	c := a.Ctx()

	mp := generic.NewMap2[render3d.Transform3D, render3d.GlobalTransform](c.World())
	tr := render3d.XForm().At(-1.4, 0.5, 0)
	e := mp.NewWith(&tr, &render3d.GlobalTransform{})
	scene.Spawn(c, "crate1", e)

	st.loadSceneSource()
	if st.srcErr != nil {
		t.Fatalf("loadSceneSource: %v", st.srcErr)
	}
	return c, st, e, path
}

func TestWritebackRewritesSceneFile(t *testing.T) {
	c, st, e, path := newTestEditor(t)

	// Simulate an inspector/gizmo edit: mutate the live transform, mark dirty
	// with an already-expired deadline, flush.
	tm := generic.NewMap[render3d.Transform3D](c.World())
	*tm.Get(e) = render3d.XForm().At(2, 0.5, -3).EulerDeg(0, 45, 0)
	st.dirty["crate1"] = time.Now().Add(-time.Millisecond)
	flushWriteback(c)

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `render3d.XForm().At(2, 0.5, -3).EulerDeg(0, 45, 0)`) {
		t.Fatalf("scene file not rewritten:\n%s", out)
	}
	if len(st.dirty) != 0 {
		t.Fatal("dirty entry not cleared")
	}
}

func TestWritebackHonorsDebounce(t *testing.T) {
	c, st, e, path := newTestEditor(t)
	tm := generic.NewMap[render3d.Transform3D](c.World())
	*tm.Get(e) = render3d.XForm().At(9, 9, 9)
	st.dirty["crate1"] = time.Now().Add(time.Hour) // deadline in the future
	flushWriteback(c)

	out, _ := os.ReadFile(path)
	if strings.Contains(string(out), "At(9, 9, 9)") {
		t.Fatal("flush wrote before the debounce deadline")
	}
	if len(st.dirty) != 1 {
		t.Fatal("pending entry dropped")
	}
}

func TestReloadAppliesHandEdits(t *testing.T) {
	c, st, e, path := newTestEditor(t)

	edited := strings.Replace(testScene,
		`render3d.XForm().At(-1.4, 0.5, 0)`,
		`render3d.XForm().At(5, 1, 2).Scaled(2, 2, 2)`, 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	reloadScene(c, st)
	if st.srcErr != nil {
		t.Fatalf("reload: %v", st.srcErr)
	}
	tm := generic.NewMap[render3d.Transform3D](c.World())
	got := *tm.Get(e)
	if got.Translation.X != 5 || got.Scale.X != 2 {
		t.Fatalf("live transform not updated: %+v", got)
	}
}

func TestUnparseableSceneGoesReadOnly(t *testing.T) {
	c, st, _, path := newTestEditor(t)
	if err := os.WriteFile(path, []byte("package scenes\nfunc {"), 0o644); err != nil {
		t.Fatal(err)
	}
	reloadScene(c, st)
	if st.srcErr == nil {
		t.Fatal("parse error not surfaced")
	}
	// A dirty mark while read-only must not panic or write.
	st.markTransformDirty("crate1")
	flushWriteback(c)
}
