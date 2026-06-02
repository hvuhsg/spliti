package editor_test

import (
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/hvuhsg/spliti/plugin/runtime"
	"github.com/hvuhsg/spliti/plugin/sprite"
	"github.com/hvuhsg/spliti/plugin/terminal"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/viewport"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// TestSceneLoad_ProducesEntitiesWithEditorMeta verifies that
// editor.LoadScene populates the world with entities tagged by
// EditorMeta (so the hierarchy panel can find them) and with the
// expected built-in components.
func TestSceneLoad_ProducesEntitiesWithEditorMeta(t *testing.T) {
	dir := t.TempDir()
	// Minimal scene: 2 entities, mix of components.
	sceneSrc := []byte(`
schema = 1
name = "test"

[[entity]]
name = "a"
[entity.position]
x = 1
y = 2
[entity.tag]
name = "alpha"

[[entity]]
name = "b"
[entity.position]
x = 5
y = 6
[entity.velocity]
dx = 1
dy = 0
[entity.bounds]
w = 2
h = 2
`)
	scenePath := filepath.Join(dir, "test.scene")
	if err := writeFile(scenePath, sceneSrc); err != nil {
		t.Fatal(err)
	}

	a := app.New()
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		if err := editor.LoadScene(c, scenePath); err != nil {
			t.Fatalf("load: %v", err)
		}
	})
	a.SetMaxFrames(1).Run()

	count := 0
	app.Query1[state.EditorMeta](a.Ctx(), func(_ ecs.Entity, _ *state.EditorMeta) {
		count++
	})
	if count != 2 {
		t.Fatalf("expected 2 EditorMeta entities, got %d", count)
	}

	// Verify component values applied correctly.
	var posCount, velCount, boundsCount, tagCount int
	w := a.Ctx().World()
	posMap := generic.NewMap1[tui.Position](w)
	velMap := generic.NewMap1[runtime.Velocity](w)
	bMap := generic.NewMap1[runtime.Bounds](w)
	tMap := generic.NewMap1[runtime.Tag](w)
	posID := ecs.ComponentID[tui.Position](w)
	velID := ecs.ComponentID[runtime.Velocity](w)
	bID := ecs.ComponentID[runtime.Bounds](w)
	tID := ecs.ComponentID[runtime.Tag](w)
	app.Query1[state.EditorMeta](a.Ctx(), func(e ecs.Entity, meta *state.EditorMeta) {
		if w.Has(e, posID) {
			posCount++
			p := posMap.Get(e)
			switch meta.SceneName {
			case "a":
				if p.X != 1 || p.Y != 2 {
					t.Errorf("a position = %+v, want {1,2}", p)
				}
			case "b":
				if p.X != 5 || p.Y != 6 {
					t.Errorf("b position = %+v, want {5,6}", p)
				}
			}
		}
		if w.Has(e, velID) {
			velCount++
			v := velMap.Get(e)
			if v.DX != 1 {
				t.Errorf("b velocity DX = %d, want 1", v.DX)
			}
		}
		if w.Has(e, bID) {
			boundsCount++
			b := bMap.Get(e)
			if b.W != 2 || b.H != 2 {
				t.Errorf("b bounds = %+v, want {2,2}", b)
			}
		}
		if w.Has(e, tID) {
			tagCount++
			tag := tMap.Get(e)
			if tag.Name != "alpha" {
				t.Errorf("a tag = %q, want alpha", tag.Name)
			}
		}
	})
	if posCount != 2 || velCount != 1 || boundsCount != 1 || tagCount != 1 {
		t.Fatalf("component counts: pos=%d vel=%d bounds=%d tag=%d", posCount, velCount, boundsCount, tagCount)
	}
}

// TestSceneLoad_RendersThroughViewport spawns scene entities and
// verifies they render inside the viewport rect AND nowhere else.
func TestSceneLoad_RendersThroughViewport(t *testing.T) {
	dir := t.TempDir()
	scenePath := filepath.Join(dir, "v.scene")
	src := []byte(`
schema = 1
name = "v"
[[entity]]
name = "a"
[entity.position]
x = 3
y = 1
[entity.glyph]
char = "@"

[[entity]]
name = "b"
[entity.position]
x = 100
y = 100
[entity.glyph]
char = "?"
`)
	if err := writeFile(scenePath, src); err != nil {
		t.Fatal(err)
	}

	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(40, 12)

	a := app.New()
	app.InsertResource(a, &terminal.Terminal{Screen: s})
	app.InsertResource(a, sprite.NewRegistry())
	// Viewport: a 10x6 rect at (5,2). Anything outside (in world coords)
	// must not render.
	app.InsertResource(a, &viewport.Viewport{X: 5, Y: 2, W: 10, H: 6, Active: true})
	a.AddPlugins(tui.Plugin{})
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		if err := editor.LoadScene(c, scenePath); err != nil {
			t.Fatal(err)
		}
	})
	a.SetMaxFrames(1).Run()

	cells, _, _ := s.GetContents()
	W, _ := s.Size()
	at := func(x, y int) rune {
		c := cells[y*W+x]
		if len(c.Runes) == 0 {
			return ' '
		}
		return c.Runes[0]
	}
	// Entity 'a' at world (3,1) → screen (5+3, 2+1) = (8, 3).
	if got := at(8, 3); got != '@' {
		t.Fatalf("(8,3) = %q, want '@'", got)
	}
	// Entity 'b' at world (100,100) is way out of viewport bounds. It
	// must NOT render anywhere on the screen.
	for y := 0; y < 12; y++ {
		for x := 0; x < W; x++ {
			if at(x, y) == '?' {
				t.Fatalf("entity 'b' leaked at (%d,%d)", x, y)
			}
		}
	}
	// Outside the viewport rect, nothing should have been drawn.
	// SimulationScreen starts blank (zero/space) so any non-space cell
	// outside the viewport would indicate leakage.
	for y := 0; y < 2; y++ {
		for x := 0; x < W; x++ {
			r := at(x, y)
			if r != 0 && r != ' ' {
				t.Fatalf("chrome leak at (%d,%d) = %q", x, y, r)
			}
		}
	}
}

func writeFile(path string, data []byte) error {
	return writeFileImpl(path, data)
}
