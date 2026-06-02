package project_test

import (
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/editor/project"
	"github.com/hvuhsg/spliti/plugin/sprite"
)

func TestSprite_LoadAsset_PaddleFromTOML(t *testing.T) {
	sf, err := project.LoadSpriteFile("testdata/paddle.sprite")
	if err != nil {
		t.Fatal(err)
	}
	if sf.Ref != "paddle" {
		t.Fatalf("ref = %q, want paddle", sf.Ref)
	}
	if sf.W != 1 || sf.H != 4 {
		t.Fatalf("dims = %d,%d, want 1,4", sf.W, sf.H)
	}
	asset, err := project.AssetFromFile(sf)
	if err != nil {
		t.Fatal(err)
	}
	if len(asset.Cells) != 4 {
		t.Fatalf("cells = %d, want 4", len(asset.Cells))
	}
	for i, c := range asset.Cells {
		if c.Empty {
			t.Fatalf("cell %d marked empty", i)
		}
		if c.Char != '█' {
			t.Fatalf("cell %d char = %q, want '█'", i, c.Char)
		}
	}
}

func TestSprite_RoundTrip_PreservesNonEmptyCells(t *testing.T) {
	yellow := tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	asset := &sprite.SpriteAsset{
		W: 3, H: 2,
		Cells: []sprite.Cell{
			{Char: '#', Style: yellow},
			{Empty: true},
			{Char: '#', Style: yellow},
			{Empty: true},
			{Char: '#', Style: yellow},
			{Empty: true},
		},
	}
	sf := project.FileFromAsset("zigzag", asset)
	got, err := project.AssetFromFile(sf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cells) != len(asset.Cells) {
		t.Fatalf("cell count mismatch: got %d want %d", len(got.Cells), len(asset.Cells))
	}
	for i := range asset.Cells {
		if asset.Cells[i].Empty != got.Cells[i].Empty {
			t.Fatalf("cell %d empty mismatch: got %v want %v", i, got.Cells[i].Empty, asset.Cells[i].Empty)
		}
		if !asset.Cells[i].Empty {
			if asset.Cells[i].Char != got.Cells[i].Char || asset.Cells[i].Style != got.Cells[i].Style {
				t.Fatalf("cell %d mismatch: got %+v want %+v", i, got.Cells[i], asset.Cells[i])
			}
		}
	}
}

func TestSprite_SaveLoad_File(t *testing.T) {
	dir := t.TempDir()
	sf := &project.SpriteFile{
		Ref: "ball", W: 1, H: 1,
		Style: project.StyleTOML{Fg: "yellow", Bold: true},
		Rows:  []string{"●"},
	}
	path := filepath.Join(dir, "ball.sprite")
	if err := project.SaveSpriteFile(path, sf); err != nil {
		t.Fatal(err)
	}
	got, err := project.LoadSpriteFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ref != "ball" || got.W != 1 || got.H != 1 {
		t.Fatalf("unexpected reload: %+v", got)
	}
	if got.Style.Fg != "yellow" || !got.Style.Bold {
		t.Fatalf("style lost: %+v", got.Style)
	}
}
