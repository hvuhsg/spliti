package main

import (
	"testing"
	gotime "time"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/canvas"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/terminal"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
)

// boxMap is a W x H map with a solid border and an empty interior.
func boxMap(w, h int) GameMap {
	g := make([]uint8, w*h)
	for x := 0; x < w; x++ {
		g[x] = 1
		g[(h-1)*w+x] = 1
	}
	for y := 0; y < h; y++ {
		g[y*w] = 1
		g[y*w+w-1] = 1
	}
	return GameMap{W: w, H: h, Grid: g}
}

func TestLosClear(t *testing.T) {
	m := boxMap(5, 5) // open interior 1..3
	if !losClear(m, 1.5, 2.5, 3.5, 2.5) {
		t.Fatal("expected clear LOS across open interior")
	}
	// Drop a wall in the middle of the path.
	m.Grid[2*5+2] = 1
	if losClear(m, 1.5, 2.5, 3.5, 2.5) {
		t.Fatal("expected LOS blocked by interior wall")
	}
}

func TestMoveWithCollisionSlides(t *testing.T) {
	m := boxMap(5, 5)
	// Pushing into the east border at x=4 should block X but allow Y to slide.
	x, y := moveWithCollision(m, 3.0, 2.0, 1.0, 0.5)
	if x > 3.5 {
		t.Fatalf("X should be blocked by wall, got %.2f", x)
	}
	if y <= 2.0 {
		t.Fatalf("Y should slide along the wall, got %.2f", y)
	}
}

func TestCastWallsDistance(t *testing.T) {
	cv := canvas.New(40, 20)
	g := &Game{Map: boxMap(5, 5), Depth: make([]float64, cv.CW)}
	// Stand at (1.5,1.5) looking due east; the center ray should hit the wall
	// plane at x=4, i.e. perpendicular distance 2.5.
	p := &Player{X: 1.5, Y: 1.5, DirX: 1, DirY: 0, PlaneX: 0, PlaneY: 0.66}
	castWalls(cv, g, p)

	got := g.Depth[cv.CW/2]
	if got < 2.4 || got > 2.6 {
		t.Fatalf("center perp distance = %.3f, want ~2.5", got)
	}
	// All depths must be positive and finite.
	for i, d := range g.Depth {
		if d <= 0 || d > 100 {
			t.Fatalf("depth[%d] = %v out of range", i, d)
		}
	}
}

// withSimDoom wires the full game against a SimulationScreen so it can run
// headlessly. It mirrors main() but uses individual plugins instead of
// defaultplugins (which would open a real terminal).
func withSimDoom(t *testing.T, w, h int) (*app.App, tcell.SimulationScreen) {
	t.Helper()
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatalf("sim init: %v", err)
	}
	s.SetSize(w, h)

	a := app.New()
	splititime.Plugin{FixedTimestep: 16 * gotime.Millisecond, TargetFrameRate: 60}.Build(a)
	app.InsertResource(a, &terminal.Terminal{Screen: s})
	input.Plugin{}.Build(a)
	tui.Plugin{}.Build(a)

	app.InsertResource(a, &Player{})
	app.InsertResource(a, &Hold{})
	app.InsertResource(a, &Game{})
	app.InitState(a, Playing)
	app.OnEnter(a, Playing, setupLevel)
	a.AddSystems(schedule.Update, handleInput)
	a.AddSystems(schedule.FixedUpdate, step)
	tui.AddOverlay(a, render)
	return a, s
}

func TestRenderSmoke(t *testing.T) {
	sizes := [][2]int{
		{80, 40}, // normal
		{40, 9},  // small but renderable
		{10, 5},  // too small -> guard path
		{120, 60},
	}
	for _, sz := range sizes {
		a, s := withSimDoom(t, sz[0], sz[1])
		a.SetMaxFrames(3).Run() // must not panic at any size
		_ = s
	}
}

func TestRenderDrawsWalls(t *testing.T) {
	a, s := withSimDoom(t, 80, 40)
	a.SetMaxFrames(3).Run()

	// The canvas blit fills the view with upper-half-block runes; confirm the
	// 3D view actually rendered something.
	cells, _, _ := s.GetContents()
	w, _ := s.Size()
	found := false
	for y := 0; y < 5 && !found; y++ {
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) > 0 && c.Runes[0] == '▀' {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("expected upper-half-block glyphs from the canvas blit")
	}
}
