package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/sprite"
	"github.com/hvuhsg/spliti/plugin/terminal"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/viewport"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/generic"
)

// withSimScreen wires a SimulationScreen into the App as the Terminal
// resource so the tui plugin can render against it without a real terminal.
func withSimScreen(t *testing.T, w, h int) (*app.App, tcell.SimulationScreen) {
	t.Helper()
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatalf("sim screen init: %v", err)
	}
	s.SetSize(w, h)
	a := app.New()
	app.InsertResource(a, &terminal.Terminal{Screen: s})
	// Deliberately not calling s.Fini() — Fini wipes the simulation screen's
	// front buffer, which we want to inspect after Run() returns.
	a.AddPlugins(tui.Plugin{})
	return a, s
}

// readCell extracts the first rune in a cell, ignoring combining runes.
func readCell(s tcell.SimulationScreen, x, y int) rune {
	cells, _, _ := s.GetContents()
	w, _ := s.Size()
	c := cells[y*w+x]
	if len(c.Runes) == 0 {
		return ' '
	}
	return c.Runes[0]
}

func TestRender_FastPath_NoLayer(t *testing.T) {
	a, s := withSimScreen(t, 10, 4)
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap2[tui.Position, tui.Glyph](c.World())
		m.NewWith(&tui.Position{X: 2, Y: 1}, &tui.Glyph{Char: '@'})
		m.NewWith(&tui.Position{X: 5, Y: 2}, &tui.Glyph{Char: '#'})
	})
	a.SetMaxFrames(1).Run()

	if got := readCell(s, 2, 1); got != '@' {
		t.Fatalf("(2,1)=%q, want '@'", got)
	}
	if got := readCell(s, 5, 2); got != '#' {
		t.Fatalf("(5,2)=%q, want '#'", got)
	}
}

func TestRender_LayerDrawsOnTopOfUnlayered(t *testing.T) {
	a, s := withSimScreen(t, 10, 4)
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		// Unlayered entity at (3,1).
		m := generic.NewMap2[tui.Position, tui.Glyph](c.World())
		m.NewWith(&tui.Position{X: 3, Y: 1}, &tui.Glyph{Char: 'A'})

		// Layered entity at the same cell — should win.
		m3 := generic.NewMap3[tui.Position, tui.Glyph, tui.Layer](c.World())
		m3.NewWith(&tui.Position{X: 3, Y: 1}, &tui.Glyph{Char: 'B'}, &tui.Layer{Z: 1})
	})
	a.SetMaxFrames(1).Run()

	if got := readCell(s, 3, 1); got != 'B' {
		t.Fatalf("(3,1)=%q, want 'B' (layered should overdraw unlayered)", got)
	}
}

func TestRender_HigherZWinsAcrossLayered(t *testing.T) {
	a, s := withSimScreen(t, 10, 4)
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap3[tui.Position, tui.Glyph, tui.Layer](c.World())
		// Spawn three layered entities at the same cell with different Z.
		// Highest Z must be visible.
		m.NewWith(&tui.Position{X: 4, Y: 2}, &tui.Glyph{Char: 'L'}, &tui.Layer{Z: 5})
		m.NewWith(&tui.Position{X: 4, Y: 2}, &tui.Glyph{Char: 'M'}, &tui.Layer{Z: 1})
		m.NewWith(&tui.Position{X: 4, Y: 2}, &tui.Glyph{Char: 'H'}, &tui.Layer{Z: 9})
	})
	a.SetMaxFrames(1).Run()

	if got := readCell(s, 4, 2); got != 'H' {
		t.Fatalf("(4,2)=%q, want 'H' (highest Z should win)", got)
	}
}

func TestRender_Viewport_ClipsAndOffsets(t *testing.T) {
	a, s := withSimScreen(t, 20, 8)
	// Viewport occupies the right-half rectangle (10..20 x 2..7).
	app.InsertResource(a, &viewport.Viewport{X: 10, Y: 2, W: 10, H: 5, Active: true})
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap2[tui.Position, tui.Glyph](c.World())
		// World coords (3,1) should land at screen (13,3).
		m.NewWith(&tui.Position{X: 3, Y: 1}, &tui.Glyph{Char: 'X'})
		// World coords (15,2) is outside the viewport's [0,10) world width — must NOT render.
		m.NewWith(&tui.Position{X: 15, Y: 2}, &tui.Glyph{Char: 'Y'})
	})
	a.SetMaxFrames(1).Run()
	if got := readCell(s, 13, 3); got != 'X' {
		t.Fatalf("(13,3) screen = %q, want 'X'", got)
	}
	// Outside the viewport: cell (5,3) is in screen-coords land but lies
	// in the editor's chrome area — the renderer should NOT have cleared
	// or written there. The simulation screen starts at zero values
	// (' '), so any value other than ' ' indicates leakage.
	if got := readCell(s, 5, 3); got != ' ' && got != 0 {
		t.Fatalf("chrome area (5,3) was overwritten by renderer: %q", got)
	}
	// Inside the viewport but outside any entity: should have been cleared
	// to a space.
	if got := readCell(s, 11, 3); got != ' ' {
		t.Fatalf("viewport interior (11,3) = %q, want ' '", got)
	}
}

func TestRender_Sprite_DrawsCellsAtPosition(t *testing.T) {
	a, s := withSimScreen(t, 12, 4)
	reg := sprite.NewRegistry()
	reg.Set("dot3", &sprite.SpriteAsset{
		W: 3, H: 1,
		Cells: []sprite.Cell{
			{Char: 'a'},
			{Char: 'b'},
			{Char: 'c'},
		},
	})
	app.InsertResource(a, reg)
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap2[tui.Position, sprite.Sprite](c.World())
		m.NewWith(&tui.Position{X: 2, Y: 1}, &sprite.Sprite{Ref: "dot3"})
	})
	a.SetMaxFrames(1).Run()
	for i, want := range []rune{'a', 'b', 'c'} {
		if got := readCell(s, 2+i, 1); got != want {
			t.Fatalf("(%d,1) = %q, want %q", 2+i, got, want)
		}
	}
}

func TestRender_Sprite_LayerSortsAgainstGlyph(t *testing.T) {
	a, s := withSimScreen(t, 10, 4)
	reg := sprite.NewRegistry()
	reg.Set("solid", &sprite.SpriteAsset{
		W: 1, H: 1,
		Cells: []sprite.Cell{{Char: 'S'}},
	})
	app.InsertResource(a, reg)
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		// Layered glyph at z=5.
		gm := generic.NewMap3[tui.Position, tui.Glyph, tui.Layer](c.World())
		gm.NewWith(&tui.Position{X: 4, Y: 2}, &tui.Glyph{Char: 'G'}, &tui.Layer{Z: 5})
		// Layered sprite at z=10 at the same cell — sprite should win.
		sm := generic.NewMap3[tui.Position, sprite.Sprite, tui.Layer](c.World())
		sm.NewWith(&tui.Position{X: 4, Y: 2}, &sprite.Sprite{Ref: "solid"}, &tui.Layer{Z: 10})
	})
	a.SetMaxFrames(1).Run()
	if got := readCell(s, 4, 2); got != 'S' {
		t.Fatalf("(4,2) = %q, want 'S' (higher-Z sprite must overdraw glyph)", got)
	}
}

func TestRender_Sprite_EmptyCellsTransparent(t *testing.T) {
	a, s := withSimScreen(t, 10, 4)
	reg := sprite.NewRegistry()
	reg.Set("dotted", &sprite.SpriteAsset{
		W: 3, H: 1,
		Cells: []sprite.Cell{
			{Char: 'a'},
			{Empty: true},
			{Char: 'c'},
		},
	})
	app.InsertResource(a, reg)
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		// Plain glyph in the gap that the sprite leaves transparent.
		gm := generic.NewMap2[tui.Position, tui.Glyph](c.World())
		gm.NewWith(&tui.Position{X: 1, Y: 1}, &tui.Glyph{Char: 'B'})
		sm := generic.NewMap2[tui.Position, sprite.Sprite](c.World())
		sm.NewWith(&tui.Position{X: 0, Y: 1}, &sprite.Sprite{Ref: "dotted"})
	})
	a.SetMaxFrames(1).Run()
	if got := readCell(s, 0, 1); got != 'a' {
		t.Fatalf("(0,1) = %q, want 'a'", got)
	}
	if got := readCell(s, 1, 1); got != 'B' {
		t.Fatalf("(1,1) = %q, want 'B' (transparent sprite cell must reveal glyph below)", got)
	}
	if got := readCell(s, 2, 1); got != 'c' {
		t.Fatalf("(2,1) = %q, want 'c'", got)
	}
}

func TestRender_LayeredOffscreenIgnored(t *testing.T) {
	a, s := withSimScreen(t, 4, 4)
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap3[tui.Position, tui.Glyph, tui.Layer](c.World())
		// Out of bounds; should be silently dropped, not panic.
		m.NewWith(&tui.Position{X: 100, Y: 100}, &tui.Glyph{Char: 'X'}, &tui.Layer{Z: 1})
		// In bounds, should render.
		m.NewWith(&tui.Position{X: 1, Y: 1}, &tui.Glyph{Char: '+'}, &tui.Layer{Z: 1})
	})
	a.SetMaxFrames(1).Run()

	if got := readCell(s, 1, 1); got != '+' {
		t.Fatalf("(1,1)=%q, want '+'", got)
	}
}
