package time_test

import (
	gotime "time"

	"testing"

	"github.com/hvuhsg/spliti/app"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"
)

func TestTimeDeltaIsPositiveAfterFirstTick(t *testing.T) {
	a := app.New().AddPlugins(splititime.Plugin{FixedTimestep: gotime.Hour})

	var d gotime.Duration
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		d = app.GetResource[splititime.Time](c).Delta()
	}).SetMaxFrames(2).Run()

	if d <= 0 {
		t.Fatalf("delta=%v, want >0", d)
	}
}

func TestFixedUpdateRunsMoreThanUpdate(t *testing.T) {
	// Tiny fixed step so accumulator fires multiple times per frame.
	a := app.New().AddPlugins(splititime.Plugin{FixedTimestep: 100 * gotime.Microsecond})

	var fixedRuns, updateRuns int
	a.AddSystems(schedule.Update, func(*app.Ctx) {
		updateRuns++
		// Burn a little time so accumulator advances.
		gotime.Sleep(2 * gotime.Millisecond)
	})
	a.AddSystems(schedule.FixedUpdate, func(*app.Ctx) {
		fixedRuns++
	})
	a.SetMaxFrames(5).Run()

	if updateRuns != 5 {
		t.Fatalf("update ran %d times, want 5", updateRuns)
	}
	// With ~10ms total update sleep and 100µs step, expect dozens of fixed runs.
	if fixedRuns <= updateRuns {
		t.Fatalf("fixedRuns=%d, updateRuns=%d, want fixedRuns > updateRuns", fixedRuns, updateRuns)
	}
}

func TestFixedUpdateRespectsAccumulatorCap(t *testing.T) {
	// Sanity: even after a deliberate long stall, fixed loop doesn't run forever.
	a := app.New().AddPlugins(splititime.Plugin{FixedTimestep: gotime.Millisecond})

	var fixedRuns int
	a.AddSystems(schedule.Update, func(*app.Ctx) {
		gotime.Sleep(50 * gotime.Millisecond)
	})
	a.AddSystems(schedule.FixedUpdate, func(*app.Ctx) { fixedRuns++ })
	a.SetMaxFrames(1).Run()

	// Cap is 4 fixed steps per frame.
	if fixedRuns > 4 {
		t.Fatalf("fixedRuns=%d, want <=4 (accumulator should be capped)", fixedRuns)
	}
}
