// Package gamepad polls connected game controllers and exposes them through
// the backend-agnostic plugin/inputs types, on native desktop (GLFW) and in
// the browser (the Gamepad API) alike.
//
// Add Plugin{} to the app and read input either by polling the
// *inputs.Gamepads resource (held buttons, analog axes) or through
// plugin/inputs/actions bindings (actions.Pad / actions.PadAxis), which read
// the same resource. Connection and button edges are also published as
// inputs.GamepadConnectionEvent / inputs.GamepadButtonEvent, sent from
// schedule.First so they're visible for the whole frame like the other input
// events.
//
// Only controllers the platform recognizes as standard dual-stick gamepads
// are reported (GLFW's gamepad mappings on native, mapping=="standard" in the
// browser); exotic joysticks without a mapping are ignored. Browsers hide
// gamepads from a page until the user presses a button while it has focus.
//
// On native the plugin uses GLFW, which wants its calls on the main OS
// thread; Build locks the goroutine to it, same as the GPU backends. When no
// display is available (headless CI) the plugin logs once and reports no
// pads.
package gamepad

import (
	"log"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/schedule"
)

// Plugin installs the *inputs.Gamepads resource and a per-frame poll system
// (label "__spliti_gamepad_poll", schedule.First).
type Plugin struct {
	Logf func(format string, args ...any) // warnings; nil = log.Printf
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	logf := p.Logf
	if logf == nil {
		logf = log.Printf
	}

	pads := &inputs.Gamepads{}
	app.InsertResource(a, pads)

	ok := platformInit(logf)
	var prev inputs.Gamepads
	a.AddSystems(schedule.First, app.System(func(c *app.Ctx) {
		if !ok {
			return
		}
		platformPoll(pads)
		emitEdges(c, &prev, pads)
		prev = *pads
	}).Label("__spliti_gamepad_poll"))
}

// emitEdges publishes connection and button-transition events by diffing the
// previous frame's state against the current one. Buttons still held when a
// pad disconnects get a synthetic Release so event consumers never see a
// stuck button.
func emitEdges(c *app.Ctx, prev, cur *inputs.Gamepads) {
	for i := range cur.Pads {
		was, is := &prev.Pads[i], &cur.Pads[i]
		id := inputs.GamepadID(i)
		if was.Connected != is.Connected {
			app.SendEvent(c, inputs.GamepadConnectionEvent{
				ID: id, Connected: is.Connected, Name: is.Name,
			})
		}
		for b := range is.Buttons {
			wasDown := was.Connected && was.Buttons[b]
			isDown := is.Connected && is.Buttons[b]
			if wasDown == isDown {
				continue
			}
			action := inputs.Press
			if wasDown {
				action = inputs.Release
			}
			app.SendEvent(c, inputs.GamepadButtonEvent{
				ID: id, Button: inputs.GamepadButton(b), Action: action,
			})
		}
	}
}
