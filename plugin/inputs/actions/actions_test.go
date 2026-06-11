package actions_test

import (
	"testing"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/inputs/actions"
	"github.com/hvuhsg/spliti/schedule"
)

// frameState samples the action state observed during one frame's Update.
type frameState struct {
	held, justPressed, justReleased bool
	axis                            float32
}

// runFrames runs an app for len(events) frames. Each frame, events[i] is sent
// from First (like a GPU backend would), and the action state for the given
// action is sampled in Update.
func runFrames(t *testing.T, m *actions.Map, action actions.Action, events [][]any) []frameState {
	t.Helper()
	var out []frameState
	frame := 0
	app.New().
		AddPlugins(actions.Plugin{Map: m}).
		AddSystems(schedule.First, func(c *app.Ctx) {
			for _, ev := range events[frame] {
				switch e := ev.(type) {
				case inputs.KeyEvent:
					app.SendEvent(c, e)
				case inputs.MouseButtonEvent:
					app.SendEvent(c, e)
				}
			}
		}).
		AddSystems(schedule.Update, func(c *app.Ctx) {
			in := actions.Get(c)
			out = append(out, frameState{
				held:         in.Held(action),
				justPressed:  in.JustPressed(action),
				justReleased: in.JustReleased(action),
				axis:         in.Axis(action),
			})
			frame++
		}).
		SetMaxFrames(len(events)).
		Run()
	return out
}

func TestHeldAndEdgesAcrossFrames(t *testing.T) {
	m := actions.NewMap()
	m.Bind("jump", actions.Key(inputs.KeySpace))

	got := runFrames(t, m, "jump", [][]any{
		{inputs.KeyEvent{Key: inputs.KeySpace, Action: inputs.Press}},
		{}, // still held, no events
		{inputs.KeyEvent{Key: inputs.KeySpace, Action: inputs.Repeat}}, // repeat keeps held
		{inputs.KeyEvent{Key: inputs.KeySpace, Action: inputs.Release}},
		{},
	})

	want := []frameState{
		{held: true, justPressed: true},
		{held: true},
		{held: true},
		{justReleased: true},
		{},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("frame %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestMultipleSourcesAnyActivates(t *testing.T) {
	m := actions.NewMap()
	m.Bind("fire", actions.Key(inputs.KeyF), actions.Mouse(inputs.MouseButtonLeft))

	got := runFrames(t, m, "fire", [][]any{
		{inputs.MouseButtonEvent{Button: inputs.MouseButtonLeft, Action: inputs.Press}},
		// Key press while mouse held: stays held, no new edge.
		{inputs.KeyEvent{Key: inputs.KeyF, Action: inputs.Press}},
		// Mouse released but key still down: held with no release edge.
		{inputs.MouseButtonEvent{Button: inputs.MouseButtonLeft, Action: inputs.Release}},
		{inputs.KeyEvent{Key: inputs.KeyF, Action: inputs.Release}},
	})

	want := []frameState{
		{held: true, justPressed: true},
		{held: true},
		{held: true},
		{justReleased: true},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("frame %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestButtonAxis(t *testing.T) {
	m := actions.NewMap()
	m.BindAxis("move-x", actions.ButtonAxis(actions.Key(inputs.KeyA), actions.Key(inputs.KeyD)))

	got := runFrames(t, m, "move-x", [][]any{
		{inputs.KeyEvent{Key: inputs.KeyD, Action: inputs.Press}},
		{inputs.KeyEvent{Key: inputs.KeyA, Action: inputs.Press}}, // both held cancel out
		{inputs.KeyEvent{Key: inputs.KeyD, Action: inputs.Release}},
		{inputs.KeyEvent{Key: inputs.KeyA, Action: inputs.Release}},
	})

	wantAxis := []float32{1, 0, -1, 0}
	for i, w := range wantAxis {
		if got[i].axis != w {
			t.Errorf("frame %d: axis = %v, want %v", i, got[i].axis, w)
		}
	}
}

func TestPadSourcesReadGamepadsResource(t *testing.T) {
	m := actions.NewMap()
	m.Bind("jump", actions.Pad(inputs.GamepadA))
	m.BindAxis("move-x", actions.PadAxis(inputs.AxisLeftX))
	m.BindAxis("move-y", actions.PadAxis(inputs.AxisLeftY).Inverted())

	pads := &inputs.Gamepads{}
	var jump, prevJump []bool
	var x, y []float32

	frame := 0
	a := app.New().
		AddPlugins(actions.Plugin{Map: m}).
		AddSystems(schedule.First, func(c *app.Ctx) {
			switch frame {
			case 0: // connect with A held and stick pushed
				pads.Pads[2] = inputs.GamepadState{Connected: true, Name: "pad"}
				pads.Pads[2].Buttons[inputs.GamepadA] = true
				pads.Pads[2].Axes[inputs.AxisLeftX] = 0.5
				pads.Pads[2].Axes[inputs.AxisLeftY] = 0.05 // below dead zone
			case 1: // release
				pads.Pads[2].Buttons[inputs.GamepadA] = false
				pads.Pads[2].Axes[inputs.AxisLeftX] = 0
				pads.Pads[2].Axes[inputs.AxisLeftY] = -0.8
			}
		}).
		AddSystems(schedule.Update, func(c *app.Ctx) {
			in := actions.Get(c)
			jump = append(jump, in.Held("jump"))
			prevJump = append(prevJump, in.JustPressed("jump"))
			x = append(x, in.Axis("move-x"))
			y = append(y, in.Axis("move-y"))
			frame++
		}).
		SetMaxFrames(2)
	app.InsertResource(a, pads)
	a.Run()

	if !jump[0] || !prevJump[0] || jump[1] {
		t.Errorf("jump: held=%v justPressed=%v, want held+justPressed frame 0 only", jump, prevJump)
	}
	if x[0] != 0.5 || x[1] != 0 {
		t.Errorf("move-x = %v, want [0.5 0]", x)
	}
	if y[0] != 0 || y[1] != 0.8 { // dead-zoned then inverted
		t.Errorf("move-y = %v, want [0 0.8]", y)
	}
}
