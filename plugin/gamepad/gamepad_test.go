package gamepad

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/schedule"
)

func TestEmitEdgesDiffsConnectionsAndButtons(t *testing.T) {
	var conns []inputs.GamepadConnectionEvent
	var btns []inputs.GamepadButtonEvent

	var prev, cur inputs.Gamepads
	frame := 0
	app.New().
		AddSystems(schedule.First, func(c *app.Ctx) {
			switch frame {
			case 0: // pad 3 connects with A already down
				cur.Pads[3] = inputs.GamepadState{Connected: true, Name: "test pad"}
				cur.Pads[3].Buttons[inputs.GamepadA] = true
			case 1: // A released, B pressed
				cur.Pads[3].Buttons[inputs.GamepadA] = false
				cur.Pads[3].Buttons[inputs.GamepadB] = true
			case 2: // pad unplugged mid-press
				cur.Pads[3] = inputs.GamepadState{}
			}
			emitEdges(c, &prev, &cur)
			prev = cur
		}).
		AddSystems(schedule.Update, func(c *app.Ctx) {
			conns = append(conns, app.ReadEvents[inputs.GamepadConnectionEvent](c)...)
			btns = append(btns, app.ReadEvents[inputs.GamepadButtonEvent](c)...)
			frame++
		}).
		SetMaxFrames(3).
		Run()

	wantConns := []inputs.GamepadConnectionEvent{
		{ID: 3, Connected: true, Name: "test pad"},
		{ID: 3, Connected: false},
	}
	if len(conns) != len(wantConns) || conns[0] != wantConns[0] || conns[1] != wantConns[1] {
		t.Errorf("connection events = %+v, want %+v", conns, wantConns)
	}

	wantBtns := []inputs.GamepadButtonEvent{
		{ID: 3, Button: inputs.GamepadA, Action: inputs.Press},
		{ID: 3, Button: inputs.GamepadA, Action: inputs.Release},
		{ID: 3, Button: inputs.GamepadB, Action: inputs.Press},
		// synthetic release: B was held when the pad disconnected
		{ID: 3, Button: inputs.GamepadB, Action: inputs.Release},
	}
	if len(btns) != len(wantBtns) {
		t.Fatalf("button events = %+v, want %+v", btns, wantBtns)
	}
	for i := range wantBtns {
		if btns[i] != wantBtns[i] {
			t.Errorf("button event %d = %+v, want %+v", i, btns[i], wantBtns[i])
		}
	}
}
