// Package input forwards tcell events into the spliti event system.
// It depends on the terminal plugin: add terminal.Plugin{} before input.Plugin{}.
package input

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/terminal"
	"github.com/hvuhsg/spliti/schedule"
)

// KeyEvent is sent for every key press.
type KeyEvent struct {
	Key  tcell.Key
	Rune rune
	Mod  tcell.ModMask
}

// ResizeEvent is sent when the terminal is resized.
type ResizeEvent struct{ Width, Height int }

// MouseEvent is sent when mouse input is enabled and the user moves/clicks.
type MouseEvent struct {
	X, Y    int
	Buttons tcell.ButtonMask
	Mod     tcell.ModMask
}

// Plugin spawns a goroutine that polls tcell events and forwards them as
// spliti events in the First stage of each frame.
type Plugin struct{}

// Build implements app.Plugin.
func (Plugin) Build(a *app.App) {
	term := app.GetResource[terminal.Terminal](a.Ctx())
	if term == nil {
		panic("input.Plugin requires terminal.Plugin to be added first")
	}
	screen := term.Screen

	ch := make(chan tcell.Event, 64)
	quit := make(chan struct{})

	go func() {
		for {
			ev := screen.PollEvent()
			if ev == nil {
				return
			}
			select {
			case ch <- ev:
			case <-quit:
				return
			}
		}
	}()

	a.AddOnExit(func() {
		close(quit)
		// Inject a synthetic event so PollEvent returns and the goroutine exits.
		screen.PostEvent(tcell.NewEventInterrupt(nil))
	})

	a.AddSystems(schedule.First, app.System(func(c *app.Ctx) {
		for {
			select {
			case ev := <-ch:
				switch e := ev.(type) {
				case *tcell.EventKey:
					app.SendEvent(c, KeyEvent{
						Key:  e.Key(),
						Rune: e.Rune(),
						Mod:  e.Modifiers(),
					})
				case *tcell.EventResize:
					w, h := e.Size()
					app.SendEvent(c, ResizeEvent{Width: w, Height: h})
				case *tcell.EventMouse:
					x, y := e.Position()
					app.SendEvent(c, MouseEvent{
						X: x, Y: y,
						Buttons: e.Buttons(),
						Mod:     e.Modifiers(),
					})
				}
			default:
				return
			}
		}
	}).Label("__spliti_input_drain"))
}
