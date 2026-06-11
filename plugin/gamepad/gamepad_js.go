//go:build js

package gamepad

import (
	"syscall/js"

	"github.com/hvuhsg/spliti/plugin/inputs"
)

// platformInit checks for the browser Gamepad API. No thread pinning needed:
// wasm runs single-threaded and the poll happens inside the frame callback.
func platformInit(logf func(string, ...any)) bool {
	nav := js.Global().Get("navigator")
	if !nav.Truthy() || !nav.Get("getGamepads").Truthy() {
		logf("gamepad: browser lacks the Gamepad API; gamepads disabled")
		return false
	}
	return true
}

// stdButtons maps the W3C "standard" mapping's button indices to the
// canonical inputs.GamepadButton values (which follow GLFW's order). Indices
// 6 and 7 are the analog triggers, surfaced as axes instead.
var stdButtons = [17]inputs.GamepadButton{
	0:  inputs.GamepadA,
	1:  inputs.GamepadB,
	2:  inputs.GamepadX,
	3:  inputs.GamepadY,
	4:  inputs.GamepadLeftBumper,
	5:  inputs.GamepadRightBumper,
	6:  -1, // left trigger → AxisLeftTrigger
	7:  -1, // right trigger → AxisRightTrigger
	8:  inputs.GamepadBack,
	9:  inputs.GamepadStart,
	10: inputs.GamepadLeftThumb,
	11: inputs.GamepadRightThumb,
	12: inputs.GamepadDpadUp,
	13: inputs.GamepadDpadDown,
	14: inputs.GamepadDpadLeft,
	15: inputs.GamepadDpadRight,
	16: inputs.GamepadGuide,
}

// platformPoll samples navigator.getGamepads() into pads. Pads without the
// "standard" mapping are skipped — their button order is device-specific.
func platformPoll(pads *inputs.Gamepads) {
	list := js.Global().Get("navigator").Call("getGamepads")
	n := 0
	if list.Truthy() {
		n = list.Length()
	}
	for i := 0; i < inputs.MaxGamepads; i++ {
		out := &pads.Pads[i]
		if i >= n {
			*out = inputs.GamepadState{}
			continue
		}
		gp := list.Index(i)
		if !gp.Truthy() || !gp.Get("connected").Bool() || gp.Get("mapping").String() != "standard" {
			*out = inputs.GamepadState{}
			continue
		}
		if !out.Connected {
			out.Name = gp.Get("id").String()
		}
		out.Connected = true

		buttons := gp.Get("buttons")
		bn := buttons.Length()
		for std, ours := range stdButtons {
			if ours >= 0 && std < bn {
				out.Buttons[ours] = buttons.Index(std).Get("pressed").Bool()
			}
		}
		axes := gp.Get("axes")
		an := axes.Length()
		for a := 0; a < 4 && a < an; a++ {
			out.Axes[a] = float32(axes.Index(a).Float())
		}
		if bn > 6 {
			out.Axes[inputs.AxisLeftTrigger] = float32(buttons.Index(6).Get("value").Float())
		}
		if bn > 7 {
			out.Axes[inputs.AxisRightTrigger] = float32(buttons.Index(7).Get("value").Float())
		}
	}
}
