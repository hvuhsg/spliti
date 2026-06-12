package srcmodel

import (
	"math"
	"strings"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

const sceneSrc = `package scenes

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/scene"

	"demo/game/components"
	"demo/game/entities"
)

// Main is the demo scene. This comment must survive every rewrite.
//
//spliti:scene Main
func Main(c *app.Ctx) {
	_ = scene.Spawn(c, "ground", entities.SpawnGround(c, render3d.XForm()))
	crate1 := scene.Spawn(c, "crate1", entities.SpawnCrate(c,
		render3d.XForm().At(-1.4, 0.6, 0).EulerDeg(0, 30, 0))) // keep my crate comment
	lamp := scene.Spawn(c, "lamp", entities.SpawnLamp(c, render3d.XForm().At(0, 1.8, 0).Scaled(2, 2, 2)))
	scene.Parent(c, lamp, crate1)
	scene.Set(c, crate1, components.Health{Max: 50, Current: 50})

	// hand-written code the editor must never touch
	for i := 0; i < 3; i++ {
		scene.Spawn(c, dynamicName(i), entities.SpawnCrate(c, render3d.XForm().At(float32(i), 0, 0)))
	}
	_ = scene.Spawn(c, "weird", entities.SpawnCrate(c, someTransform()))
	_ = scene.Spawn(c, "quat", entities.SpawnCrate(c, render3d.XForm().Rot(0, 0.7071068, 0, 0.7071068)))
}
`

func parseTestScene(t *testing.T) *Scene {
	t.Helper()
	f, err := ParseSceneSource([]byte(sceneSrc), "scenes/main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := f.Scene("Main")
	if s == nil {
		t.Fatal("scene Main not found")
	}
	return s
}

func TestParseRecognizesSpawns(t *testing.T) {
	s := parseTestScene(t)
	var names []string
	for _, sp := range s.Spawns {
		names = append(names, sp.Instance)
	}
	// The loop spawn has a non-literal name and must NOT be recognized.
	want := []string{"ground", "crate1", "lamp", "weird", "quat"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("recognized %v, want %v", names, want)
	}

	crate := s.Spawn("crate1")
	if crate.Var != "crate1" || crate.Prefab != "entities.SpawnCrate" {
		t.Fatalf("crate1 parsed as %+v", crate)
	}
	tr := crate.Transform
	if tr == nil || !tr.Editable {
		t.Fatalf("crate1 transform not editable: %+v", tr)
	}
	if *tr.At != [3]float32{-1.4, 0.6, 0} || *tr.EulerDeg != [3]float32{0, 30, 0} || tr.Scaled != nil {
		t.Fatalf("crate1 transform = %+v", tr)
	}

	if w := s.Spawn("weird"); w.Transform == nil || w.Transform.Editable {
		t.Fatal("non-chain transform must parse as non-editable")
	}
	if q := s.Spawn("quat"); q.Transform == nil || !q.Transform.Editable || q.Transform.Rot == nil {
		t.Fatalf("quat transform = %+v", s.Spawn("quat").Transform)
	}
}

func TestPrintIsByteIdenticalWithoutEdits(t *testing.T) {
	f, err := ParseSceneSource([]byte(sceneSrc), "scenes/main.go")
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != sceneSrc {
		t.Fatalf("parse→print changed the file:\n%s", string(out))
	}
}

func TestSetTransformRewritesOnlyTheChain(t *testing.T) {
	s := parseTestScene(t)
	err := s.SetTransform("crate1", Transform{
		At:       &[3]float32{2.5, 0.6, -1},
		EulerDeg: &[3]float32{0, 45, 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.file.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	if !strings.Contains(src, `render3d.XForm().At(2.5, 0.6, -1).EulerDeg(0, 45, 0)`) {
		t.Fatalf("new chain missing:\n%s", src)
	}
	for _, keep := range []string{
		"// keep my crate comment",
		"// Main is the demo scene. This comment must survive every rewrite.",
		"// hand-written code the editor must never touch",
		"for i := 0; i < 3; i++ {",
		"scene.Set(c, crate1, components.Health{Max: 50, Current: 50})",
		`render3d.XForm().At(0, 1.8, 0).Scaled(2, 2, 2)`,
	} {
		if !strings.Contains(src, keep) {
			t.Errorf("rewrite lost %q", keep)
		}
	}

	// The result must re-parse to the same model (the full round trip).
	f2, err := ParseSceneSource(out, "scenes/main.go")
	if err != nil {
		t.Fatal(err)
	}
	tr := f2.Scene("Main").Spawn("crate1").Transform
	if tr == nil || !tr.Editable || *tr.At != [3]float32{2.5, 0.6, -1} || *tr.EulerDeg != [3]float32{0, 45, 0} {
		t.Fatalf("re-parse after rewrite = %+v", tr)
	}
}

func TestSetTransformRejectsNonEditable(t *testing.T) {
	s := parseTestScene(t)
	if err := s.SetTransform("weird", Transform{}); err == nil {
		t.Fatal("expected error for non-literal chain")
	}
	if err := s.SetTransform("nope", Transform{}); err == nil {
		t.Fatal("expected error for unknown instance")
	}
}

func TestAddSpawnAppendsLine(t *testing.T) {
	s := parseTestScene(t)
	_, err := s.AddSpawn("crate2", "entities.SpawnCrate", Transform{
		At: &[3]float32{1, 0, 3}, Scaled: &[3]float32{0.5, 0.5, 0.5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSpawn("crate2", "entities.SpawnCrate", Transform{}); err == nil {
		t.Fatal("duplicate instance name must be rejected")
	}
	out, err := s.file.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	if !strings.Contains(src, `_ = scene.Spawn(c, "crate2", entities.SpawnCrate(c, render3d.XForm().At(1, 0, 3).Scaled(0.5, 0.5, 0.5)))`) {
		t.Fatalf("spawn line missing:\n%s", src)
	}
	f2, err := ParseSceneSource(out, "scenes/main.go")
	if err != nil {
		t.Fatal(err)
	}
	sp := f2.Scene("Main").Spawn("crate2")
	if sp == nil || *sp.Transform.At != [3]float32{1, 0, 3} {
		t.Fatalf("re-parse of added spawn = %+v", sp)
	}
}

func TestFromTransform3DCanonicalForms(t *testing.T) {
	// Identity → empty chain.
	if tr := FromTransform3D(render3d.XForm()); tr.At != nil || tr.EulerDeg != nil || tr.Rot != nil || tr.Scaled != nil {
		t.Fatalf("identity emitted calls: %+v", tr)
	}

	// Euler-representable rotation → EulerDeg, not Rot.
	v := render3d.XForm().At(1, 2, 3).EulerDeg(10, 20, 30).Scaled(2, 1, 1)
	tr := FromTransform3D(v)
	if tr.At == nil || tr.EulerDeg == nil || tr.Rot != nil || tr.Scaled == nil {
		t.Fatalf("forms = %+v", tr)
	}
	if *tr.EulerDeg != [3]float32{10, 20, 30} {
		t.Fatalf("EulerDeg = %v", *tr.EulerDeg)
	}

	// The emitted chain must evaluate back to the same transform.
	back := tr.Value()
	if back.Translation != v.Translation || back.Scale != v.Scale {
		t.Fatalf("value round trip: %+v vs %+v", back, v)
	}
	if !quatClose(back.Rotation, v.Rotation) {
		t.Fatalf("rotation round trip: %+v vs %+v", back.Rotation, v.Rotation)
	}

	// An arbitrary axis rotation should still round-trip via Euler or Rot.
	q := m.FromAxisAngle(m.Vec3{X: 0.3, Y: 0.8, Z: 0.52}.Normalize(), float32(1.234))
	v2 := render3d.Transform3D{Translation: m.Vec3{}, Rotation: q, Scale: m.Vec3{X: 1, Y: 1, Z: 1}}
	tr2 := FromTransform3D(v2)
	if !quatClose(tr2.Value().Rotation, q) {
		t.Fatalf("arbitrary rotation lost: %+v", tr2)
	}
	_ = math.Pi
}
