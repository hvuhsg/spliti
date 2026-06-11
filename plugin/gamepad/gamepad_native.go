//go:build !js

package gamepad

import (
	"runtime"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/plugin/inputs"
)

// platformInit makes GLFW usable for joystick polling. glfw.Init is
// ref-counted in C GLFW (a second call after success is a cheap no-op), so
// this coexists with a GPU backend that already initialized it; standalone
// (e.g. a terminal game) it performs the real init. Returns false — and the
// plugin reports no pads — when no display is available.
func platformInit(logf func(string, ...any)) bool {
	runtime.LockOSThread() // GLFW wants init and polling on one OS thread
	if err := glfw.Init(); err != nil {
		logf("gamepad: glfw init failed (%v); gamepads disabled", err)
		return false
	}
	return true
}

// platformPoll samples every joystick slot that GLFW recognizes as a gamepad
// (i.e. has a button/axis mapping) into pads. Runs on the main goroutine in
// schedule.First.
func platformPoll(pads *inputs.Gamepads) {
	for i := 0; i < inputs.MaxGamepads; i++ {
		jid := glfw.Joystick(i)
		out := &pads.Pads[i]
		if !jid.Present() || !jid.IsGamepad() {
			*out = inputs.GamepadState{}
			continue
		}
		st := jid.GetGamepadState()
		if st == nil {
			*out = inputs.GamepadState{}
			continue
		}
		if !out.Connected {
			out.Name = jid.GetGamepadName()
		}
		out.Connected = true
		for b := range out.Buttons {
			out.Buttons[b] = st.Buttons[b] == glfw.Press
		}
		// Stick axes pass through; GLFW reports triggers in [-1,1] while the
		// canonical inputs range is [0,1].
		out.Axes[inputs.AxisLeftX] = st.Axes[glfw.AxisLeftX]
		out.Axes[inputs.AxisLeftY] = st.Axes[glfw.AxisLeftY]
		out.Axes[inputs.AxisRightX] = st.Axes[glfw.AxisRightX]
		out.Axes[inputs.AxisRightY] = st.Axes[glfw.AxisRightY]
		out.Axes[inputs.AxisLeftTrigger] = (st.Axes[glfw.AxisLeftTrigger] + 1) / 2
		out.Axes[inputs.AxisRightTrigger] = (st.Axes[glfw.AxisRightTrigger] + 1) / 2
	}
}
