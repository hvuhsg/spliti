// Command hexisland is the game entry point. It builds natively and for
// js/wasm; the spliti editor runs the same packages through the generated
// .spliti/editor target instead.
package main

import (
	"runtime"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/audio"
	"github.com/hvuhsg/spliti/plugin/inputs/actions"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"

	"hexisland/game"
	"hexisland/game/scenes"
	"hexisland/game/systems"
)

func init() { runtime.LockOSThread() }

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{},
		render3d.Plugin{
			Width: 1280, Height: 720,
			Title:      "hexisland",
			ClearColor: render3d.Color{R: 0.53, G: 0.74, B: 0.92, A: 1}, // sky
			Ambient:    m.Vec3{X: 0.42, Y: 0.44, Z: 0.48},
			Samples:    4,
			VSync:      true,
		},
		actions.Plugin{Map: game.BuildActions()},
		audio.Plugin{},
	)
	game.RegisterSystems(a)
	a.AddSystems(schedule.Startup, app.Chain(
		app.System(game.LoadAssets),
		app.System(systems.LoadSounds),
		app.System(scenes.Main),
	))
	addCapture(a) // optional screenshot driver (SPLITI_FPS_SHOT); inert otherwise
	a.Run()
}
