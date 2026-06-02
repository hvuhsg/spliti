package panels

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/state"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/tui"
)

// Toolbar holds Play/Pause/Step/Stop/Save buttons above the viewport.
// Clicks on each button transition the editor's Mode state or trigger
// Save (Phase H). v1 lays out the buttons and recognises clicks but
// the actual play/stop logic is implemented in Phase I.
type Toolbar struct{}

// toolbarButton describes one button: label and action key.
type toolbarButton struct {
	Label string
	// Action is one of "play", "pause", "step", "stop", "save".
	Action string
}

var toolbarButtons = []toolbarButton{
	{Label: " ▶ Play  ", Action: "play"},
	{Label: " ⏸ Pause ", Action: "pause"},
	{Label: " ⏭ Step  ", Action: "step"},
	{Label: " ⏹ Stop  ", Action: "stop"},
	{Label: " 💾 Save ", Action: "save"},
}

// Name implements ui.Panel.
func (Toolbar) Name() string { return ui.NameToolbar }

// Title implements ui.Panel.
func (Toolbar) Title() string { return "" }

// Render implements ui.Panel.
func (Toolbar) Render(c *app.Ctx, r ui.Rect, _ bool) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	bg := tcell.StyleDefault.Background(tcell.ColorDarkBlue).Foreground(tcell.ColorWhite)
	ui.DrawFill(s, r, ' ', bg)

	mode := state.Editing
	if ms := app.GetState[state.Mode](c); ms != nil {
		mode = ms.Get()
	}

	x := r.X + 1
	for _, b := range toolbarButtons {
		style := bg
		if buttonActive(b.Action, mode) {
			style = bg.Bold(true).Foreground(tcell.ColorYellow)
		}
		ui.DrawText(s, x, r.Y, style, b.Label)
		x += len(b.Label) + 1
	}
}

func buttonActive(action string, mode state.Mode) bool {
	switch action {
	case "play":
		return mode == state.Playing
	case "pause":
		return mode == state.Paused
	case "stop":
		return mode == state.Editing
	}
	return false
}

// OnMouse implements ui.Panel. The toolbar maps mouse clicks to actions.
// Action handlers are wired up by the editor plugin via the resource
// pattern: clicking sets ToolbarAction; an editor system reads it.
func (Toolbar) OnMouse(c *app.Ctx, lx, _ int, ev input.MouseEvent) {
	if ev.Buttons&tcell.Button1 == 0 {
		return
	}
	x := 1
	for _, b := range toolbarButtons {
		end := x + len(b.Label)
		if lx >= x && lx < end {
			req := app.GetResource[ToolbarAction](c)
			if req != nil {
				req.Action = b.Action
			}
			return
		}
		x = end + 1
	}
}

// OnKey implements ui.Panel. Toolbar buttons are also reachable via
// global shortcuts handled in editor.go: F5 Play, F6 Pause, F7 Step,
// F8 Stop, Ctrl-S Save. Returning false here lets those bubble up.
func (Toolbar) OnKey(_ *app.Ctx, _ input.KeyEvent) bool { return false }

// ToolbarAction is the cross-frame channel between the toolbar's click
// handler and the editor system that actually applies it. The handler
// system reads & clears Action each frame; "" means "nothing pending".
type ToolbarAction struct {
	Action string
}
