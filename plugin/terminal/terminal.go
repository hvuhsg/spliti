// Package terminal owns the tcell Screen used by the input and tui plugins.
// It registers a *Terminal resource at Startup and finalizes the screen on
// app exit so the user's terminal state is always restored.
package terminal

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
)

// Terminal is the shared screen resource. Plugins access it via
// app.GetResource[Terminal].
type Terminal struct {
	Screen tcell.Screen
}

// Plugin installs the Terminal resource and its cleanup hook.
type Plugin struct {
	// EnableMouse turns on mouse event reporting.
	EnableMouse bool
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	s, err := tcell.NewScreen()
	if err != nil {
		panic(err)
	}
	if err := s.Init(); err != nil {
		panic(err)
	}
	if p.EnableMouse {
		s.EnableMouse()
	}
	s.Clear()

	t := &Terminal{Screen: s}
	app.InsertResource(a, t)
	a.AddOnExit(func() {
		s.Fini()
	})
}
