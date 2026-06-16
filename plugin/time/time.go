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

// Alpha is the interpolation factor in [0,1) between the previous and current
// fixed-update states. It is the fraction of a fixed step that has elapsed since
// the last FixedUpdate ran (fixedAccum / fixedTimestep, measured after the fixed
// loop consumed its whole steps for this frame).
//
// Render-time systems use it to draw lerp(prevState, curState, Alpha()) so a sim
// running slower than the display (e.g. 64 Hz sim on a 120 Hz panel) still moves
// smoothly instead of stepping. Read it from Update/PostUpdate; during a
// FixedUpdate step it is whatever the previous frame left.
func (t *Time) Alpha() float64 {
	if t.fixedTimestep <= 0 {
		return 0
	}
	return float64(t.fixedAccum) / float64(t.fixedTimestep)
}

// Plugin installs the Time resource, the per-frame ticker, the FixedUpdate
// loop driver, and (optionally) frame pacing.
type Plugin struct {
	// FixedTimestep is the duration of one FixedUpdate iteration. Zero uses
	// the 64 Hz default.
	FixedTimestep gotime.Duration
	// TargetFrameRate caps the main loop. Zero disables pacing (uncapped).
	// Ignored when Manual is set (a virtual clock paces nothing).
	TargetFrameRate int
	// Manual switches the clock from the wall clock to a deterministic virtual
	// clock: every frame advances Delta by exactly one fixed timestep, so one
	// frame is one FixedUpdate iteration regardless of how much real time
	// passed, and frame pacing is disabled. Headless, reproducible runs (e.g.
	// `spliti check`) set this so the same scripted inputs and RNG seed produce
	// byte-identical state every run. Combine with plugin/rng for full
	// determinism. Real-time games leave it false.
	Manual bool
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

	if p.Manual {
		// Virtual clock: advance by exactly one fixed step per frame, so the
		// FixedUpdate driver below runs precisely one iteration each frame and
		// nothing depends on the wall clock.
		a.AddSystems(schedule.First, app.System(func(*app.Ctx) {
			t.delta = t.fixedTimestep
			t.elapsed += t.delta
		}).Label("__spliti_time_tick"))
	} else {
		a.AddSystems(schedule.First, app.System(func(*app.Ctx) {
			n := gotime.Now()
			t.delta = n.Sub(t.lastTick)
			t.elapsed = n.Sub(t.started)
			t.lastTick = n
		}).Label("__spliti_time_tick"))
	}

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

	if p.TargetFrameRate > 0 && !p.Manual {
		targetDur := gotime.Second / gotime.Duration(p.TargetFrameRate)
		a.SetPostUpdateHook(func() {
			elapsed := gotime.Since(t.lastTick)
			if elapsed < targetDur {
				gotime.Sleep(targetDur - elapsed)
			}
		})
	}
}
