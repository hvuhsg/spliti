package runtime_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/runtime"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// runFrame executes one full frame (no FixedUpdate hook here because the
// time plugin would tie us to wall-clock pacing; we drive FixedUpdate
// manually via App.RunStage in tests).
func runFrameWithFixed(a *app.App) {
	a.SetMaxFrames(1)
	// We need FixedUpdate to actually execute. Install a preUpdate hook
	// that runs FixedUpdate exactly once per frame.
	a.SetPreUpdateHook(func() { a.RunStage(schedule.FixedUpdate) })
	a.Run()
}

func TestMovement_AppliesVelocityToPosition(t *testing.T) {
	a := app.New()
	a.AddPlugins(runtime.Plugin{})
	var e1Pos *tui.Position
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap2[tui.Position, runtime.Velocity](c.World())
		m.NewWith(&tui.Position{X: 5, Y: 5}, &runtime.Velocity{DX: 2, DY: -1})
	})
	a.AddSystems(schedule.Last, func(c *app.Ctx) {
		app.Query2[tui.Position, runtime.Velocity](c, func(_ ecs.Entity, p *tui.Position, _ *runtime.Velocity) {
			e1Pos = p
		})
	})
	runFrameWithFixed(a)
	if e1Pos == nil || e1Pos.X != 7 || e1Pos.Y != 4 {
		t.Fatalf("expected (7,4), got %+v", e1Pos)
	}
}

func TestKeyboard_RuneBinding_RefreshesHoldAndMoves(t *testing.T) {
	a := app.New()
	a.AddPlugins(runtime.Plugin{})
	var posPtr *tui.Position
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap2[tui.Position, runtime.KeyboardControl](c.World())
		m.NewWith(
			&tui.Position{X: 5, Y: 5},
			&runtime.KeyboardControl{
				Bindings: []runtime.KeyBinding{{
					Key: tcell.KeyRune, Rune: 'd', DX: 1, DY: 0, HoldTicks: 1,
				}},
			},
		)
	})
	a.AddSystems(schedule.First, func(c *app.Ctx) {
		// Inject a 'd' KeyEvent before runtime systems read it.
		app.SendEvent(c, input.KeyEvent{Key: tcell.KeyRune, Rune: 'd'})
	})
	a.AddSystems(schedule.Last, func(c *app.Ctx) {
		app.Query2[tui.Position, runtime.KeyboardControl](c, func(_ ecs.Entity, p *tui.Position, _ *runtime.KeyboardControl) {
			posPtr = p
		})
	})
	runFrameWithFixed(a)
	if posPtr == nil || posPtr.X != 6 || posPtr.Y != 5 {
		t.Fatalf("expected (6,5) after pressing 'd', got %+v", posPtr)
	}
}

func TestCollision_OverlappingBoxesEmitEvent(t *testing.T) {
	a := app.New()
	a.AddPlugins(runtime.Plugin{})
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap3[tui.Position, runtime.Bounds, runtime.Tag](c.World())
		m.NewWith(&tui.Position{X: 0, Y: 0}, &runtime.Bounds{W: 3, H: 3}, &runtime.Tag{Name: "a"})
		m.NewWith(&tui.Position{X: 2, Y: 2}, &runtime.Bounds{W: 3, H: 3}, &runtime.Tag{Name: "b"})
		m.NewWith(&tui.Position{X: 100, Y: 100}, &runtime.Bounds{W: 1, H: 1}, &runtime.Tag{Name: "c"})
	})
	var got []runtime.CollisionEvent
	a.AddSystems(schedule.Last, func(c *app.Ctx) {
		got = append(got, app.ReadEvents[runtime.CollisionEvent](c)...)
	})
	runFrameWithFixed(a)
	if len(got) != 1 {
		t.Fatalf("expected 1 collision event, got %d: %+v", len(got), got)
	}
	if !((got[0].ATag == "a" && got[0].BTag == "b") || (got[0].ATag == "b" && got[0].BTag == "a")) {
		t.Fatalf("unexpected tags: %+v", got[0])
	}
}

func TestPlugin_RunIfFalse_SuppressesAllSystems(t *testing.T) {
	a := app.New()
	a.AddPlugins(runtime.Plugin{RunIf: func(*app.Ctx) bool { return false }})
	var posPtr *tui.Position
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		m := generic.NewMap2[tui.Position, runtime.Velocity](c.World())
		m.NewWith(&tui.Position{X: 5, Y: 5}, &runtime.Velocity{DX: 99, DY: 99})
	})
	a.AddSystems(schedule.Last, func(c *app.Ctx) {
		app.Query2[tui.Position, runtime.Velocity](c, func(_ ecs.Entity, p *tui.Position, _ *runtime.Velocity) {
			posPtr = p
		})
	})
	runFrameWithFixed(a)
	if posPtr == nil || posPtr.X != 5 || posPtr.Y != 5 {
		t.Fatalf("expected (5,5) — RunIf=false should freeze movement, got %+v", posPtr)
	}
}
