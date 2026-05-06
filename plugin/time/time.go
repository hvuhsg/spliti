// Package time provides the spliti Time resource and the FixedUpdate fixed-
// timestep loop. Add Plugin{} to enable; defaults to 60 fps target with a
// 64 Hz fixed step.
package time

import (
	gotime "time"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
)

// Time is the per-frame timing resource installed by Plugin.
type Time struct {
	delta         gotime.Duration
	elapsed       gotime.Duration
	fixedTimestep gotime.Duration
	fixedAccum    gotime.Duration

	started  gotime.Time
	lastTick gotime.Time
}

// Delta is the wall-clock duration since the previous Update tick.
func (t *Time) Delta() gotime.Duration { return t.delta }

// Elapsed is the wall-clock duration since App.Run was called.
func (t *Time) Elapsed() gotime.Duration { return t.elapsed }

// FixedDelta is the configured fixed timestep duration.
func (t *Time) FixedDelta() gotime.Duration { return t.fixedTimestep }

// Plugin installs the Time resource, the per-frame ticker, the FixedUpdate
// loop driver, and (optionally) frame pacing.
type Plugin struct {
	// FixedTimestep is the duration of one FixedUpdate iteration. Zero uses
	// the 64 Hz default.
	FixedTimestep gotime.Duration
	// TargetFrameRate caps the main loop. Zero disables pacing (uncapped).
	TargetFrameRate int
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	fixed := p.FixedTimestep
	if fixed == 0 {
		fixed = gotime.Second / 64
	}
	now := gotime.Now()
	t := &Time{
		fixedTimestep: fixed,
		started:       now,
		lastTick:      now,
	}
	app.InsertResource(a, t)

	a.AddSystems(schedule.First, app.System(func(*app.Ctx) {
		n := gotime.Now()
		t.delta = n.Sub(t.lastTick)
		t.elapsed = n.Sub(t.started)
		t.lastTick = n
	}).Label("__spliti_time_tick"))

	a.SetPreUpdateHook(func() {
		t.fixedAccum += t.delta
		// Cap the accumulator to avoid spiral-of-death after a long stall.
		if t.fixedAccum > 4*t.fixedTimestep {
			t.fixedAccum = 4 * t.fixedTimestep
		}
		for t.fixedAccum >= t.fixedTimestep {
			a.RunStage(schedule.FixedFirst)
			a.RunStage(schedule.FixedUpdate)
			a.RunStage(schedule.FixedLast)
			t.fixedAccum -= t.fixedTimestep
		}
	})

	if p.TargetFrameRate > 0 {
		targetDur := gotime.Second / gotime.Duration(p.TargetFrameRate)
		a.SetPostUpdateHook(func() {
			elapsed := gotime.Since(t.lastTick)
			if elapsed < targetDur {
				gotime.Sleep(targetDur - elapsed)
			}
		})
	}
}
