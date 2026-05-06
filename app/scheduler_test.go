package app_test

import (
	"strings"
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
)

func TestAfterReordersSystems(t *testing.T) {
	var order []string
	app.New().
		AddSystems(schedule.Update,
			app.System(func(*app.Ctx) { order = append(order, "b") }).Label("b").After("a"),
			app.System(func(*app.Ctx) { order = append(order, "a") }).Label("a"),
		).
		SetMaxFrames(1).
		Run()

	got := strings.Join(order, ",")
	if got != "a,b" {
		t.Fatalf("got %q, want a,b", got)
	}
}

func TestBeforeReordersSystems(t *testing.T) {
	var order []string
	app.New().
		AddSystems(schedule.Update,
			app.System(func(*app.Ctx) { order = append(order, "b") }).Label("b"),
			app.System(func(*app.Ctx) { order = append(order, "a") }).Label("a").Before("b"),
		).
		SetMaxFrames(1).
		Run()

	got := strings.Join(order, ",")
	if got != "a,b" {
		t.Fatalf("got %q, want a,b", got)
	}
}

func TestChainOrdersSystems(t *testing.T) {
	var order []string
	app.New().
		AddSystems(schedule.Update, app.Chain(
			app.System(func(*app.Ctx) { order = append(order, "x") }),
			app.System(func(*app.Ctx) { order = append(order, "y") }),
			app.System(func(*app.Ctx) { order = append(order, "z") }),
		)).
		SetMaxFrames(1).
		Run()

	got := strings.Join(order, ",")
	if got != "x,y,z" {
		t.Fatalf("got %q, want x,y,z", got)
	}
}

func TestRunIfGatesExecution(t *testing.T) {
	var ran int
	frame := 0
	app.New().
		AddSystems(schedule.First, func(*app.Ctx) { frame++ }).
		AddSystems(schedule.Update,
			app.System(func(*app.Ctx) { ran++ }).
				RunIf(func(*app.Ctx) bool { return frame%2 == 0 }),
		).
		SetMaxFrames(10).
		Run()

	// First system increments frame from 1..10. Update sees frame%2==0 on
	// frames 2, 4, 6, 8, 10 → 5 runs.
	if ran != 5 {
		t.Fatalf("ran=%d, want 5", ran)
	}
}

func TestCycleDetectionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from cycle detection")
		}
	}()

	app.New().
		AddSystems(schedule.Update,
			app.System(func(*app.Ctx) {}).Label("a").After("b"),
			app.System(func(*app.Ctx) {}).Label("b").After("a"),
		).
		SetMaxFrames(1).
		Run()
}

func TestUnknownLabelPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from unknown label reference")
		}
	}()

	app.New().
		AddSystems(schedule.Update,
			app.System(func(*app.Ctx) {}).Label("a").After("nonexistent"),
		).
		SetMaxFrames(1).
		Run()
}
