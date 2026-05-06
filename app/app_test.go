package app_test

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
)

func TestRunLoopExecutesStartupOnceAndUpdateNTimes(t *testing.T) {
	var startups, updates int
	app.New().
		AddSystems(schedule.Startup, func(*app.Ctx) { startups++ }).
		AddSystems(schedule.Update, func(*app.Ctx) { updates++ }).
		SetMaxFrames(5).
		Run()

	if startups != 1 {
		t.Fatalf("startup ran %d times, want 1", startups)
	}
	if updates != 5 {
		t.Fatalf("update ran %d times, want 5", updates)
	}
}

func TestMainStagesRunInOrder(t *testing.T) {
	var order []schedule.Stage
	record := func(s schedule.Stage) app.SystemFunc {
		return func(*app.Ctx) { order = append(order, s) }
	}
	app.New().
		AddSystems(schedule.Last, record(schedule.Last)).
		AddSystems(schedule.First, record(schedule.First)).
		AddSystems(schedule.PostUpdate, record(schedule.PostUpdate)).
		AddSystems(schedule.Update, record(schedule.Update)).
		AddSystems(schedule.PreUpdate, record(schedule.PreUpdate)).
		SetMaxFrames(1).
		Run()

	want := []schedule.Stage{
		schedule.First, schedule.PreUpdate, schedule.Update, schedule.PostUpdate, schedule.Last,
	}
	if len(order) != len(want) {
		t.Fatalf("got %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("stage %d: got %v, want %v", i, order[i], want[i])
		}
	}
}

func TestStartupStagesRunInOrderBeforeMain(t *testing.T) {
	var order []schedule.Stage
	record := func(s schedule.Stage) app.SystemFunc {
		return func(*app.Ctx) { order = append(order, s) }
	}
	app.New().
		AddSystems(schedule.Update, record(schedule.Update)).
		AddSystems(schedule.PostStartup, record(schedule.PostStartup)).
		AddSystems(schedule.PreStartup, record(schedule.PreStartup)).
		AddSystems(schedule.Startup, record(schedule.Startup)).
		SetMaxFrames(1).
		Run()

	want := []schedule.Stage{
		schedule.PreStartup, schedule.Startup, schedule.PostStartup, schedule.Update,
	}
	if len(order) != len(want) {
		t.Fatalf("got %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("stage %d: got %v, want %v", i, order[i], want[i])
		}
	}
}

func TestStopHaltsLoop(t *testing.T) {
	var updates int
	a := app.New()
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		updates++
		if updates == 3 {
			c.App().Stop()
		}
	}).SetMaxFrames(100).Run()

	if updates != 3 {
		t.Fatalf("updates=%d, want 3 (Stop should halt loop)", updates)
	}
}

type recordingPlugin struct{ called *bool }

func (p recordingPlugin) Build(*app.App) { *p.called = true }

func TestPluginBuildIsInvoked(t *testing.T) {
	var built bool
	app.New().AddPlugins(recordingPlugin{called: &built})
	if !built {
		t.Fatalf("plugin Build was not invoked")
	}
}
