//go:build !js

package main

import (
	"os"
	"strconv"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/screenshot"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"

	"hexisland/game/systems"
)

// addCapture wires an optional, inert-by-default screenshot driver: when
// SPLITI_FPS_SHOT names a path, it lets the world run for a few seconds (so a
// wave spawns and approaches), saves one PNG, and quits. A normal `go run` is
// unaffected. Handy for headless/agent verification of the rendered scene.
func addCapture(a *app.App) {
	out := os.Getenv("SPLITI_FPS_SHOT")
	if out == "" {
		return
	}
	dwell := float32(5)
	if s, err := strconv.ParseFloat(os.Getenv("SPLITI_FPS_SHOT_SECONDS"), 32); err == nil && s > 0 {
		dwell = float32(s)
	}
	var elapsed float32
	var shot bool
	a.AddSystems(schedule.Update, app.System(func(c *app.Ctx) {
		if shot {
			c.App().Stop()
			return
		}
		// Tilt the view down a touch so the ground, dome, and approaching mobs
		// frame nicely for the screenshot.
		if p := app.GetResource[systems.Player](c); p != nil {
			p.Pitch = -0.12
		}
		if tm := app.GetResource[splititime.Time](c); tm != nil {
			elapsed += float32(tm.Delta().Seconds())
		}
		if elapsed >= dwell && screenshot.Save(c, out) {
			shot = true
		}
	}).Label("fps-capture"))
}
