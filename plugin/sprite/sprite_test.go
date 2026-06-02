package sprite_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/plugin/sprite"
)

func TestRegistry_SetGetDelete(t *testing.T) {
	r := sprite.NewRegistry()
	if r.Get("missing") != nil {
		t.Fatal("Get on missing ref should return nil")
	}
	asset := &sprite.SpriteAsset{
		W: 2, H: 1,
		Cells: []sprite.Cell{
			{Char: 'a', Style: tcell.StyleDefault},
			{Char: 'b', Style: tcell.StyleDefault},
		},
	}
	r.Set("foo", asset)
	if got := r.Get("foo"); got != asset {
		t.Fatalf("Get(foo) = %v, want %v", got, asset)
	}
	if got := asset.At(1, 0); got.Char != 'b' {
		t.Fatalf("At(1,0) = %v, want b", got.Char)
	}
	r.Delete("foo")
	if r.Get("foo") != nil {
		t.Fatal("Get after Delete should return nil")
	}
}

func TestRegistry_NilSafeGet(t *testing.T) {
	var r *sprite.SpriteRegistry
	if r.Get("anything") != nil {
		t.Fatal("Get on nil registry should return nil")
	}
	if got := r.Refs(); got != nil {
		t.Fatalf("Refs on nil registry should be nil, got %v", got)
	}
}

func TestRegistry_RefsListsAll(t *testing.T) {
	r := sprite.NewRegistry()
	r.Set("a", &sprite.SpriteAsset{})
	r.Set("b", &sprite.SpriteAsset{})
	got := r.Refs()
	if len(got) != 2 {
		t.Fatalf("expected 2 refs, got %v", got)
	}
}
