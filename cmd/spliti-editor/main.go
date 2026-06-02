// spliti-editor is the standalone TUI game-engine editor. Run it against
// a project directory:
//
//	spliti-editor --project mygame
//
// If --project is not given, the editor opens with no project loaded;
// File → New / File → Open let the user pick one. (For v1 those menu
// actions are stubs; you'll want to pass --project until they ship.)
//
// The editor is itself a spliti App: terminal+input(+mouse)+sprite+
// viewport+runtime+editor plugins, with the runtime gated by editor's
// Mode state so the world only ticks during Play.
package main

import (
	"flag"
	"fmt"
	"os"
	gotime "time"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor"
	"github.com/hvuhsg/spliti/editor/project"
	"github.com/hvuhsg/spliti/editor/ui/panels"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/runtime"
	"github.com/hvuhsg/spliti/plugin/sprite"
	"github.com/hvuhsg/spliti/plugin/terminal"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
)

func main() {
	projectDir := flag.String("project", "", "path to a spliti project directory")
	flag.Parse()

	a := app.New()

	// Order matters: terminal owns the screen; input depends on terminal;
	// sprite registers the registry resource; tui reads viewport+sprite;
	// runtime depends on input+tui+state; editor depends on everything.
	a.AddPlugins(terminal.Plugin{EnableMouse: true})
	a.AddPlugins(input.Plugin{})

	// Time plugin drives FixedUpdate. We use a 60Hz fixed step so editor
	// game-logic feels like the example games.
	a.AddPlugins(splititime.Plugin{
		FixedTimestep:   16 * gotime.Millisecond,
		TargetFrameRate: 60,
	})

	a.AddPlugins(sprite.Plugin{})
	a.AddPlugins(tui.Plugin{})

	// Editor plugin first so its state machine (Mode) is registered
	// before runtime.Plugin{}'s RunIf reads it. This isn't strictly
	// required because state lookup is lazy, but it's the natural order.
	a.AddPlugins(editor.Plugin{})

	// Runtime systems gated on Playing.
	a.AddPlugins(runtime.Plugin{RunIf: editor.MakeRuntimeRunIf()})

	// Optionally load a project on launch.
	if *projectDir != "" {
		p, err := project.LoadProject(*projectDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "load project:", err)
			os.Exit(1)
		}
		// Stash the project as a resource so panels can read it.
		app.InsertResource(a, p)

		// Load any user-defined component types BEFORE scene loading so
		// the registry knows about them when a scene file references
		// them.
		a.AddSystems(schedule.Startup, func(c *app.Ctx) {
			if err := editor.LoadCustomComponentsForProject(c, p); err != nil {
				setStatus(c, fmt.Sprintf("custom components: %v", err))
			}
		})

		// Eagerly load sprite assets at startup so the viewport renders
		// referenced sprites immediately.
		a.AddSystems(schedule.Startup, func(c *app.Ctx) {
			reg := app.GetResource[sprite.SpriteRegistry](c)
			if reg == nil {
				return
			}
			n, err := editor.LoadProjectAssets(p, reg)
			if err != nil {
				setStatus(c, fmt.Sprintf("asset load error: %v", err))
				return
			}
			setStatus(c, fmt.Sprintf("loaded %d sprites", n))
		})

		// Load the default scene if the project specifies one and a
		// matching file exists. Loaded entities get an EditorMeta tag so
		// the hierarchy panel lists them and save/load round-trips.
		a.AddSystems(schedule.Startup, func(c *app.Ctx) {
			scenePath := p.ScenePathByName(p.File.DefaultScene)
			if scenePath == "" {
				return
			}
			if err := editor.LoadScene(c, scenePath); err != nil {
				setStatus(c, fmt.Sprintf("scene load: %v", err))
				return
			}
			setStatus(c, fmt.Sprintf("loaded scene %q", p.File.DefaultScene))
		})
	}

	a.Run()
}

// setStatus writes a message to the StatusMessage resource. Avoids a
// direct dependency on tcell here.
func setStatus(c *app.Ctx, text string) {
	msg := app.GetResource[panels.StatusMessage](c)
	if msg == nil {
		return
	}
	msg.Text = text
}
