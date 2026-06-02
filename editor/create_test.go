package editor_test

import (
	"path/filepath"
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor"
	"github.com/hvuhsg/spliti/editor/components"
	"github.com/hvuhsg/spliti/editor/project"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/hvuhsg/spliti/editor/ui/panels"
	"github.com/hvuhsg/spliti/plugin/sprite"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

func TestCreateNewEntity_ProducesSelectableEntity(t *testing.T) {
	if len(components.Registry()) == 0 {
		components.RegisterBuiltins()
	}
	a := app.New()
	app.InsertResource(a, &state.Selection{})
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		editor.CreateNewEntity(c)
	})
	a.SetMaxFrames(1).Run()

	count := 0
	app.Query1[state.EditorMeta](a.Ctx(), func(_ ecs.Entity, _ *state.EditorMeta) {
		count++
	})
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	sel := app.GetResource[state.Selection](a.Ctx())
	if !sel.Active {
		t.Fatal("selection should be active")
	}
	posMap := generic.NewMap1[tui.Position](a.Ctx().World())
	if posMap.Get(sel.Entity) == nil {
		t.Fatal("created entity should have Position")
	}
}

func TestCreateNewSprite_WritesFileAndRegisters(t *testing.T) {
	if len(components.Registry()) == 0 {
		components.RegisterBuiltins()
	}

	dir := t.TempDir()
	p, err := project.NewEmptyProject(dir, "demo")
	if err != nil {
		t.Fatal(err)
	}

	a := app.New()
	app.InsertResource(a, p)
	app.InsertResource(a, sprite.NewRegistry())
	app.InsertResource(a, &panels.Prompt{})

	// Drive the two-step prompt manually by directly calling
	// finishCreateSprite via a tiny wrapper — exercises the
	// non-prompting path of the same code.
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		// Open the prompt and submit "ball" → then "1x1".
		editor.CreateNewSprite(c)
		pr := app.GetResource[panels.Prompt](c)
		if pr == nil || !pr.Active {
			t.Fatal("expected active prompt for step 1")
		}
		if pr.OnSubmit == nil {
			t.Fatal("step 1 OnSubmit nil")
		}
		pr.OnSubmit(c, "ball")

		// Step 2 prompt should now be active.
		if !pr.Active {
			t.Fatal("expected active prompt for step 2")
		}
		pr.OnSubmit(c, "1x1")
	})
	a.SetMaxFrames(1).Run()

	registry := app.GetResource[sprite.SpriteRegistry](a.Ctx())
	if registry.Get("ball") == nil {
		t.Fatal("registry missing 'ball'")
	}
	expected := filepath.Join(dir, "sprites", "ball.sprite")
	if _, err := project.LoadSpriteFile(expected); err != nil {
		t.Fatalf("sprite file not written: %v", err)
	}
}
