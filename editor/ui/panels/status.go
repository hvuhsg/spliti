package panels

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/tui"
)

// StatusMessage is a transient resource holding the last status message.
// Systems write to it; the StatusBar reads it. Empty string = idle.
type StatusMessage struct {
	Text  string
	Style tcell.Style
}

// StatusBar is the bottom row showing transient messages and shortcut
// hints. It reads StatusMessage from resources; if missing or empty,
// it shows a default hint.
type StatusBar struct{}

// Name implements ui.Panel.
func (StatusBar) Name() string { return ui.NameStatus }

// Title implements ui.Panel.
func (StatusBar) Title() string { return "" }

// Render implements ui.Panel.
func (StatusBar) Render(c *app.Ctx, r ui.Rect, _ bool) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	bg := tcell.StyleDefault.Background(tcell.ColorBlue).Foreground(tcell.ColorWhite)
	ui.DrawFill(s, r, ' ', bg)
	msg := app.GetResource[StatusMessage](c)
	text := " ^S Save   ^O Open   F5 Play   F6 Pause   ^Q Quit "
	style := bg
	if msg != nil && msg.Text != "" {
		text = " " + msg.Text + " "
		if msg.Style != (tcell.Style{}) {
			style = msg.Style
		}
	}
	ui.DrawTextClipped(s, r.X, r.Y, r.W, style, text)
}

// OnMouse implements ui.Panel.
func (StatusBar) OnMouse(_ *app.Ctx, _, _ int, _ input.MouseEvent) {}

// OnKey implements ui.Panel.
func (StatusBar) OnKey(_ *app.Ctx, _ input.KeyEvent) bool { return false }
