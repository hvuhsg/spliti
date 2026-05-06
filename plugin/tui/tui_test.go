package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/terminal"
	"github.com/hvuhsg/spliti/plugin/tui"
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
