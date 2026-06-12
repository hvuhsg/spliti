package registry

import (
	"reflect"
	"testing"

	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

type health struct {
	Max, Current int
	Regen        float32
	hidden       bool //nolint:unused // exercises the unexported-field skip
}

type fancy struct {
	Pos    m.Vec3
	Target ecs.Entity
	Inner  struct {
		Speed float32
		Name  string
	}
	Blob map[string]int // no editor kind: opaque
}

func TestRegisterAndWorldAccess(t *testing.T) {
	r := New()
	Register[health](r, "Health", "components")
	w := ecs.NewWorld()
	mp := generic.NewMap1[health](&w)
	e := mp.NewWith(&health{Max: 50, Current: 30})

	ti := r.Lookup("Health")
	if ti == nil || !ti.Has(&w, e) {
		t.Fatal("registered type not found on entity")
	}
	v := ti.Get(&w, e)
	v.FieldByName("Current").SetInt(45)
	if got := mp.Get(e).Current; got != 45 {
		t.Fatalf("reflect write did not hit live storage: %d", got)
	}
	ti.Remove(&w, e)
	if ti.Has(&w, e) {
		t.Fatal("Remove left component behind")
	}
	ti.Add(&w, e)
	if !ti.Has(&w, e) || mp.Get(e).Max != 0 {
		t.Fatal("Add did not attach zero value")
	}
}

func TestFieldWalk(t *testing.T) {
	r := New()
	Register[fancy](r, "Fancy", "components")
	fields := r.Lookup("Fancy").Fields

	want := map[string]FieldKind{
		"Pos":         KindVec3,
		"Target":      KindEntity,
		"Inner.Speed": KindFloat32,
		"Inner.Name":  KindString,
		"Blob":        KindOpaque,
	}
	if len(fields) != len(want) {
		t.Fatalf("fields = %+v", fields)
	}
	comp := reflect.New(reflect.TypeFor[fancy]()).Elem()
	for _, f := range fields {
		k, ok := want[f.Name]
		if !ok || f.Kind != k {
			t.Errorf("field %q kind %v", f.Name, f.Kind)
		}
		f.Value(comp) // must not panic
	}
}

func TestOnFiltersHidden(t *testing.T) {
	r := New()
	Builtin(r)
	w := ecs.NewWorld()
	mp := generic.NewMap2[render3d.Transform3D, render3d.GlobalTransform](&w)
	tr := render3d.XForm()
	e := mp.NewWith(&tr, &render3d.GlobalTransform{})

	on := r.On(&w, e)
	if len(on) != 1 || on[0].Name != "Transform3D" {
		names := make([]string, len(on))
		for i, ti := range on {
			names[i] = ti.Name
		}
		t.Fatalf("On = %v, want [Transform3D]", names)
	}
}
