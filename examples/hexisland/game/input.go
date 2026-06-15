package game

import (
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/inputs/actions"
)

// BuildActions is the game's input table: logical actions bound to physical
// keys, mouse buttons, and gamepad input. The editor's Input panel edits this
// function in place; game systems read it via actions.Get(c).
//
//spliti:input
func BuildActions() *actions.Map {
	m := actions.NewMap()
	m.Bind("jump", actions.Key(inputs.KeySpace), actions.Pad(inputs.GamepadA))
	m.BindAxis("move-x",
		actions.ButtonAxis(actions.Key(inputs.KeyA), actions.Key(inputs.KeyD)),
		actions.PadAxis(inputs.AxisLeftX))
	m.BindAxis("move-y",
		actions.ButtonAxis(actions.Key(inputs.KeyS), actions.Key(inputs.KeyW)),
		actions.PadAxis(inputs.AxisLeftY).Inverted())
	return m
}
