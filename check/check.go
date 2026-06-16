// Package check is spliti's headless verification harness — the "run + observe"
// machinery of the agent loop (write → run → observe → correct).
//
// Run drives a fully-configured App for a fixed number of frames and captures
// the resulting world as JSON (via package inspect), optionally writing a frame
// screenshot. Combined with the deterministic virtual clock (plugin/time Manual
// mode), a seeded plugin/rng, and a Script of inputs, the same program produces
// byte-identical state every run — so an agent can edit gameplay code, re-run,
// and diff the state to confirm a change with no window and no human.
//
// The harness itself needs no GPU: a logic-only App (no render backend) runs
// anywhere, including CI. Games that wire a render backend run wherever that
// backend runs, and Run still captures the world JSON regardless.
package check

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/inspect"
	"github.com/hvuhsg/spliti/schedule"
)

// Options configures a check run.
type Options struct {
	// Ticks is how many frames to run (must be >= 1). With plugin/time in
	// Manual mode this is exactly that many fixed-update steps.
	Ticks int
	// Script feeds deterministic input. Nil means no synthetic input.
	Script *Script
	// WorldPath, when set, receives the final world dump as indented JSON.
	WorldPath string
	// ScreenshotPath, when set together with Capture, receives a PNG of the
	// final frame.
	ScreenshotPath string
	// Capture, when set, is invoked on the final frame with ScreenshotPath to
	// save a frame image. It is injected (rather than imported) so the harness
	// stays free of the GPU/cgo render backend: a logic-only run leaves it nil;
	// `spliti check` passes screenshot.Save. It returns whether the request was
	// registered.
	Capture func(c *app.Ctx, path string) bool
}

// Result is the outcome of a check run.
type Result struct {
	// Dump is the world captured after the final frame.
	Dump inspect.Dump
	// JSON is Dump as indented JSON (the same bytes written to WorldPath).
	JSON []byte
	// Frames is how many frames actually ran.
	Frames int
}

// Run drives a configured App headlessly for opts.Ticks frames, then captures
// the world. The App should already have its plugins and systems installed;
// Run only adds the input script, the optional screenshot request, and the
// frame bound. For reproducibility the App should use plugin/time Manual mode
// and a seeded plugin/rng.
func Run(a *app.App, opts Options) (Result, error) {
	if opts.Ticks < 1 {
		return Result{}, fmt.Errorf("check: Ticks must be >= 1, got %d", opts.Ticks)
	}

	if opts.Script != nil {
		opts.Script.install(a)
	}
	if opts.ScreenshotPath != "" && opts.Capture != nil {
		final := opts.Ticks - 1
		a.AddSystems(schedule.First, app.System(func(c *app.Ctx) {
			// Request the capture during the final frame so that frame's own
			// present writes it (CaptureFrame fulfils at the next present).
			if a.Frame() == final {
				opts.Capture(c, opts.ScreenshotPath)
			}
		}).Label("__spliti_check_screenshot"))
	}

	a.SetMaxFrames(opts.Ticks)
	a.Run()

	dump := inspect.World(a.Ctx())
	b, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("check: marshal world: %w", err)
	}
	if opts.WorldPath != "" {
		if err := os.WriteFile(opts.WorldPath, b, 0o644); err != nil {
			return Result{}, fmt.Errorf("check: write %s: %w", opts.WorldPath, err)
		}
	}
	return Result{Dump: dump, JSON: b, Frames: a.Frame()}, nil
}
