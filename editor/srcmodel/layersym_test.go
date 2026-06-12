package srcmodel

import (
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/hvuhsg/spliti/plugin/collision"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// parseTestExpr parses a single Go expression into its dst form.
func parseTestExpr(t *testing.T, src string) dst.Expr {
	t.Helper()
	f, err := decorator.ParseFile(token.NewFileSet(), "expr.go", "package p\nvar x = "+src+"\n", 0)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return f.Decls[0].(*dst.GenDecl).Specs[0].(*dst.ValueSpec).Values[0]
}

func testLayersTable() *Layers {
	return &Layers{
		Pkg:        "game",
		ImportPath: "example.com/mygame/game",
		// bit 1 is unnamed (`_` in the const block).
		Names: []string{"LayerDefault", "", "LayerPlayer", "LayerEnemy"},
	}
}

func TestSetComponentValueSymbolicLayers(t *testing.T) {
	s := parseTestScene(t)
	s.SetLayers(testLayersTable())
	want := collision.Collider3D{
		Half:  m.Vec3{X: 1},
		Layer: 1 << 2,             // LayerPlayer
		Mask:  1<<0 | 1<<1 | 1<<3, // Default | unnamed bit 1 | Enemy
	}
	if err := s.SetComponentValue("crate1", "collision.Collider3D", reflect.ValueOf(want)); err != nil {
		t.Fatal(err)
	}
	src := render(t, s)
	if !strings.Contains(src, `Layer: game.LayerPlayer`) {
		t.Fatalf("single layer not symbolic:\n%s", src)
	}
	if !strings.Contains(src, `Mask: game.LayerDefault | game.LayerEnemy | 2`) {
		t.Fatalf("mask not symbolic with numeric remainder:\n%s", src)
	}
	if !strings.Contains(src, `"example.com/mygame/game"`) {
		t.Fatalf("game import missing:\n%s", src)
	}

	// Without the table the line is read-only — selectors are not literals.
	s2 := reparse(t, s)
	if sl := s2.Spawn("crate1").Set("collision.Collider3D"); sl == nil || sl.Editable {
		t.Fatalf("expected read-only without a layers table: %+v", sl)
	}

	// With the table installed it becomes editable and decodes back exactly.
	s2.SetLayers(testLayersTable())
	sl := s2.Spawn("crate1").Set("collision.Collider3D")
	if sl == nil || !sl.Editable {
		t.Fatalf("symbolic layers not editable with table: %+v", sl)
	}
	var got collision.Collider3D
	if err := ApplyLitLayers(sl.Lit(), reflect.ValueOf(&got).Elem(), nil, testLayersTable()); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("decoded %+v, want %+v", got, want)
	}

	// Decoding without the table must fail loudly, not zero silently.
	if err := ApplyLit(sl.Lit(), reflect.ValueOf(&got).Elem()); err == nil {
		t.Fatal("ApplyLit without a layers table accepted a named constant")
	}
}

func TestSymbolicLayerNumericFallbacks(t *testing.T) {
	s := parseTestScene(t)
	s.SetLayers(testLayersTable())
	// Layer is an unnamed bit; Mask holds only unnamed bits — both must fall
	// back to plain numerals.
	val := collision.Collider3D{Layer: 1 << 1, Mask: 1 << 1}
	if err := s.SetComponentValue("crate1", "collision.Collider3D", reflect.ValueOf(val)); err != nil {
		t.Fatal(err)
	}
	src := render(t, s)
	if !strings.Contains(src, `Layer: 2, Mask: 2`) {
		t.Fatalf("unnamed bits should stay numeric:\n%s", src)
	}
	if strings.Contains(src, `"example.com/mygame/game"`) {
		t.Fatalf("numeric fallback should not import the game package:\n%s", src)
	}

	// Numeric lines stay editable and decode with or without the table.
	s2 := reparse(t, s)
	sl := s2.Spawn("crate1").Set("collision.Collider3D")
	if sl == nil || !sl.Editable {
		t.Fatalf("numeric collider not editable: %+v", sl)
	}
	var got collision.Collider3D
	if err := ApplyLit(sl.Lit(), reflect.ValueOf(&got).Elem()); err != nil {
		t.Fatal(err)
	}
	if got != val {
		t.Fatalf("decoded %+v, want %+v", got, val)
	}
}

func TestLayersResolveExprForms(t *testing.T) {
	l := testLayersTable()
	cases := []struct {
		src  string
		want uint64
		ok   bool
	}{
		{"game.LayerDefault", 1, true},
		{"game.LayerEnemy", 8, true},
		{"game.LayerDefault | game.LayerPlayer", 5, true},
		{"game.LayerDefault | 2 | game.LayerEnemy", 11, true},
		{"(game.LayerDefault | game.LayerEnemy)", 9, true},
		{"game.LayerNope", 0, false},
		{"other.LayerDefault", 0, false},
		{"game.LayerDefault & game.LayerEnemy", 0, false},
	}
	for _, tc := range cases {
		e := parseTestExpr(t, tc.src)
		got, ok := l.resolveExpr(e)
		if ok != tc.ok || got != tc.want {
			t.Errorf("resolveExpr(%s) = %d,%v; want %d,%v", tc.src, got, ok, tc.want, tc.ok)
		}
	}
	// A nil table resolves nothing.
	var nilL *Layers
	if _, ok := nilL.resolveExpr(parseTestExpr(t, "game.LayerDefault")); ok {
		t.Error("nil table resolved a selector")
	}
}
