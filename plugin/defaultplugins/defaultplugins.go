// Package defaultplugins bundles the standard set of plugins for a typical
// CLI game: time + terminal + input + tui. Use Plugins{} to add them all at
// once.
package defaultplugins

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/terminal"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/tui"
)

// Plugins is the spliti equivalent of Bevy's DefaultPlugins. Add it with
// app.New().AddPlugins(defaultplugins.Plugins{}).
type Plugins struct {
	// Time configures the time plugin. Zero values yield 64Hz fixed step
	// and 60 fps target frame rate.
	Time splititime.Plugin
	// EnableMouse enables mouse input reporting on the terminal.
	EnableMouse bool
}

// Build implements app.Plugin.
func (p Plugins) Build(a *app.App) {
	t := p.Time
	if t.TargetFrameRate == 0 {
		t.TargetFrameRate = 60
	}
	t.Build(a)
	terminal.Plugin{EnableMouse: p.EnableMouse}.Build(a)
	input.Plugin{}.Build(a)
	tui.Plugin{}.Build(a)
}
