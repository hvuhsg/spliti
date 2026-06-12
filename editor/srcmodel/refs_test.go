package srcmodel

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mlange-42/arche/ecs"
)

type testLoadout struct {
	Weights []float32
	Points  []testVec
	Tags    []string
}

type testTarget struct {
	Leader ecs.Entity
	Allies []ecs.Entity
	Boost  float32
}

func TestSetComponentValueSliceRoundTrip(t *testing.T) {
	s := parseTestScene(t)
	want := testLoadout{
		Weights: []float32{1, 0.5, -2},
		Points:  []testVec{{X: 1}, {Y: 2, Z: 3}},
		Tags:    []string{"a", "b"},
	}
	if err := s.SetComponentValue("crate1", "components.Loadout", reflect.ValueOf(want)); err != nil {
		t.Fatal(err)
	}
	src := render(t, s)
	if !strings.Contains(src, `Weights: []float32{1, 0.5, -2}`) {
		t.Fatalf("float slice wrong:\n%s", src)
	}
	if !strings.Contains(src, `Points: []srcmodel.testVec{{X: 1}, {Y: 2, Z: 3}}`) {
		t.Fatalf("struct slice not elided/encoded:\n%s", src)
	}

	s2 := reparse(t, s)
	sl := s2.Spawn("crate1").Set("components.Loadout")
	if sl == nil || !sl.Editable {
		t.Fatalf("slice override not editable on re-parse: %+v", sl)
	}
	var got testLoadout
	if err := ApplyLit(sl.Lit(), reflect.ValueOf(&got).Elem()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded %+v, want %+v", got, want)
	}
}

func TestSetComponentValueEntityRefs(t *testing.T) {
	w := ecs.NewWorld()
	lamp, ground := w.NewEntity(), w.NewEntity()

	s := parseTestScene(t)
	val := testTarget{Leader: lamp, Allies: []ecs.Entity{ground}, Boost: 2}
	resolver := func(e ecs.Entity) (string, bool) {
		switch e {
		case lamp:
			return "lamp", true
		case ground:
			return "ground", true
		}
		return "", false
	}
	if err := s.SetComponentValueRefs("crate1", "components.Target", reflect.ValueOf(val), resolver); err != nil {
		t.Fatal(err)
	}
	src := render(t, s)
	// ground had no variable — the spawn must have been promoted.
	if !strings.Contains(src, `ground := scene.Spawn(c, "ground"`) {
		t.Fatalf("ground spawn not promoted to a variable:\n%s", src)
	}
	if !strings.Contains(src, `components.Target{Leader: lamp, Allies: []ecs.Entity{ground}, Boost: 2}`) {
		t.Fatalf("entity refs not encoded as variables:\n%s", src)
	}
	if !strings.Contains(src, `"github.com/mlange-42/arche/ecs"`) {
		t.Fatalf("ecs import missing for []ecs.Entity:\n%s", src)
	}

	// Round trip: still recognized AND editable (idents are known spawn vars).
	s2 := reparse(t, s)
	sl := s2.Spawn("crate1").Set("components.Target")
	if sl == nil || !sl.Editable {
		t.Fatalf("entity-ref override not editable on re-parse: %+v", sl)
	}
	look := func(varName string) (ecs.Entity, bool) {
		switch varName {
		case "lamp":
			return lamp, true
		case "ground":
			return ground, true
		}
		return ecs.Entity{}, false
	}
	var got testTarget
	if err := ApplyLitRefs(sl.Lit(), reflect.ValueOf(&got).Elem(), look); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, val) {
		t.Fatalf("decoded %+v, want %+v", got, val)
	}

	// Without a lookup the decode must fail loudly, not zero silently.
	if err := ApplyLit(sl.Lit(), reflect.ValueOf(&got).Elem()); err == nil {
		t.Fatal("ApplyLit without lookup accepted an entity reference")
	}
}

func TestSetComponentValueEntityRefRejectsUnnamed(t *testing.T) {
	w := ecs.NewWorld()
	stray := w.NewEntity()
	s := parseTestScene(t)
	val := testTarget{Leader: stray}
	err := s.SetComponentValueRefs("crate1", "components.Target", reflect.ValueOf(val),
		func(ecs.Entity) (string, bool) { return "", false })
	if err == nil {
		t.Fatal("unresolvable entity reference was accepted")
	}
	// And with no resolver at all.
	if err := s.SetComponentValue("crate1", "components.Target", reflect.ValueOf(val)); err == nil {
		t.Fatal("SetComponentValue accepted an entity reference without a resolver")
	}
}

func TestSetLineMovesBelowReferencedSpawn(t *testing.T) {
	w := ecs.NewWorld()
	quat := w.NewEntity()

	s := parseTestScene(t)
	// First write a plain override for ground: it lands right after ground's
	// spawn line, far above the "quat" spawn at the bottom.
	if err := s.SetComponentValue("ground", "components.Target", reflect.ValueOf(testTarget{Boost: 1})); err != nil {
		t.Fatal(err)
	}
	// Now point it at quat: the line must move below quat's spawn.
	resolver := func(e ecs.Entity) (string, bool) { return "quat", e == quat }
	if err := s.SetComponentValueRefs("ground", "components.Target", reflect.ValueOf(testTarget{Leader: quat}), resolver); err != nil {
		t.Fatal(err)
	}
	src := render(t, s)
	setIdx := strings.Index(src, "scene.Set(c, ground, components.Target{Leader: quat})")
	spawnIdx := strings.Index(src, `quat := scene.Spawn(c, "quat"`)
	if setIdx < 0 || spawnIdx < 0 {
		t.Fatalf("expected lines missing:\n%s", src)
	}
	if setIdx < spawnIdx {
		t.Fatalf("Set line not moved below the referenced spawn:\n%s", src)
	}
	if strings.Count(src, "components.Target{") != 1 {
		t.Fatalf("old Set line not removed:\n%s", src)
	}
	// The whole file must still re-parse with the line editable.
	s2 := reparse(t, s)
	if sl := s2.Spawn("ground").Set("components.Target"); sl == nil || !sl.Editable {
		t.Fatalf("moved line not recognized/editable: %+v", sl)
	}
}

func TestRemoveSpawnRefusesWhenEntityRefPointsAtIt(t *testing.T) {
	w := ecs.NewWorld()
	lamp := w.NewEntity()
	s := parseTestScene(t)
	resolver := func(e ecs.Entity) (string, bool) { return "lamp", e == lamp }
	if err := s.SetComponentValueRefs("crate1", "components.Target", reflect.ValueOf(testTarget{Leader: lamp}), resolver); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveSpawn("lamp"); err == nil {
		t.Fatal("RemoveSpawn deleted an instance still referenced by an entity-ref override")
	}
}
