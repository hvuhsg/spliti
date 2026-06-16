package check_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	gotime "time"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/check"
	"github.com/hvuhsg/spliti/inspect"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/inputs/actions"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

type Mover struct{ Steps int }

// buildApp wires a logic-only game: a Mover entity that advances one step each
// frame the "right" action is held. No render backend — runs anywhere.
func buildApp() *app.App {
	a := app.New()
	a.AddPlugins(splititime.Plugin{FixedTimestep: gotime.Second / 50, Manual: true})
	m := actions.NewMap()
	m.Bind("right", actions.Key(inputs.KeyD))
	a.AddPlugins(actions.Plugin{Map: m})

	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		mm := generic.NewMap1[Mover](c.World())
		mm.NewWith(&Mover{})
	})
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		if !actions.Get(c).Held("right") {
			return
		}
		app.Query1[Mover](c, func(_ ecs.Entity, mv *Mover) { mv.Steps++ })
	})
	return a
}

func moverSteps(t *testing.T, d inspect.Dump) int {
	t.Helper()
	for _, e := range d.Entities {
		if raw, ok := e.Components["check_test.Mover"]; ok {
			var mv Mover
			if err := json.Unmarshal(raw, &mv); err != nil {
				t.Fatalf("unmarshal Mover: %v", err)
			}
			return mv.Steps
		}
	}
	t.Fatal("no Mover entity in dump")
	return -1
}

func TestScriptedRunIsDeterministicAndObservable(t *testing.T) {
	dir := t.TempDir()
	worldPath := filepath.Join(dir, "world.json")

	// Hold "right" (KeyD) for frames [0,5): expect exactly 5 steps.
	script := check.NewScript().Hold(inputs.KeyD, 0, 5)
	res, err := check.Run(buildApp(), check.Options{Ticks: 10, Script: script, WorldPath: worldPath})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Frames != 10 {
		t.Fatalf("frames=%d, want 10", res.Frames)
	}
	if got := moverSteps(t, res.Dump); got != 5 {
		t.Fatalf("Mover.Steps=%d, want 5 (held frames 0..4)", got)
	}

	// world.json was written and matches the returned JSON.
	onDisk, err := os.ReadFile(worldPath)
	if err != nil {
		t.Fatalf("read world.json: %v", err)
	}
	if !bytes.Equal(onDisk, res.JSON) {
		t.Fatal("world.json on disk differs from Result.JSON")
	}

	// Same script + Manual clock → byte-identical state on a second run.
	res2, err := check.Run(buildApp(), check.Options{Ticks: 10, Script: check.NewScript().Hold(inputs.KeyD, 0, 5)})
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	if !bytes.Equal(res.JSON, res2.JSON) {
		t.Fatalf("runs diverged:\n%s\n---\n%s", res.JSON, res2.JSON)
	}
}

func TestNoInputNoMovement(t *testing.T) {
	res, err := check.Run(buildApp(), check.Options{Ticks: 8})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := moverSteps(t, res.Dump); got != 0 {
		t.Fatalf("Mover.Steps=%d, want 0 with no script", got)
	}
}

func TestTicksValidated(t *testing.T) {
	if _, err := check.Run(buildApp(), check.Options{Ticks: 0}); err == nil {
		t.Fatal("expected error for Ticks=0")
	}
}
