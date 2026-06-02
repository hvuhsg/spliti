package panels

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/tui"
)

// Picker is a generic list-modal: shows a scrollable list of named
// items, lets the user navigate with arrow keys + Enter or mouse, and
// fires OnPick with the chosen index. Esc cancels.
//
// Pickers are spawned by callers — for "Add component" the inspector
// opens one populated with missing-component names, for the future
// "Add custom-type field" the schema editor would open one with
// FieldKind options.
type Picker struct {
	Active        bool
	TitleText     string
	Items         []string
	SelectedIndex int
	OnPick        func(c *app.Ctx, index int, label string)
	OnCancel      func(c *app.Ctx)
}

// Name implements ui.Panel.
func (*Picker) Name() string { return "picker" }

// Title implements ui.Panel.
func (p *Picker) Title() string { return p.TitleText }

// Open shows the picker with the given items + callbacks.
func (p *Picker) Open(title string, items []string, onPick func(c *app.Ctx, index int, label string), onCancel func(c *app.Ctx)) {
	p.TitleText = title
	p.Items = items
	p.SelectedIndex = 0
	p.OnPick = onPick
	p.OnCancel = onCancel
	p.Active = true
}

// Close hides the picker and clears callbacks.
func (p *Picker) Close() {
	p.Active = false
	p.Items = nil
	p.OnPick = nil
	p.OnCancel = nil
}

// Render draws the picker at r. Only renders when Active.
func (p *Picker) Render(c *app.Ctx, r ui.Rect, _ bool) {
	if !p.Active {
		return
	}
	s := tui.Screen(c)
	if s == nil {
		return
	}
	bg := tcell.StyleDefault.Background(tcell.ColorBlack)
	ui.DrawFill(s, r, ' ', bg)
	ui.DrawBox(s, r, p.TitleText, true)

	inner := r.Inset(1)
	if inner.W <= 0 || inner.H <= 0 {
		return
	}
	if len(p.Items) == 0 {
		ui.DrawTextClipped(s, inner.X, inner.Y, inner.W, ui.StyleTextDim, "(no items)")
	}
	for i, item := range p.Items {
		if i >= inner.H-1 {
			break
		}
		style := ui.StyleText
		if i == p.SelectedIndex {
			style = ui.StyleSelected
			ui.DrawHLine(s, inner.X, inner.Y+i, inner.W, ' ', style)
		}
		ui.DrawTextClipped(s, inner.X+1, inner.Y+i, inner.W-1, style, item)
	}
	footY := inner.Y + inner.H - 1
	ui.DrawTextClipped(s, inner.X, footY, inner.W, ui.StyleTextDim, "↑↓: navigate   Enter: pick   Esc: cancel")
}

// OnMouse implements ui.Panel. Click selects + picks in one shot.
func (p *Picker) OnMouse(c *app.Ctx, _, ly int, ev input.MouseEvent) {
	if !p.Active || ev.Buttons&tcell.Button1 == 0 {
		return
	}
	row := ly - 1
	if row < 0 || row >= len(p.Items) {
		return
	}
	idx := row
	label := p.Items[idx]
	cb := p.OnPick
	p.Close()
	if cb != nil {
		cb(c, idx, label)
	}
}

// OnKey implements ui.Panel.
func (p *Picker) OnKey(c *app.Ctx, ev input.KeyEvent) bool {
	if !p.Active {
		return false
	}
	switch ev.Key {
	case tcell.KeyEscape:
		cb := p.OnCancel
		p.Close()
		if cb != nil {
			cb(c)
		}
		return true
	case tcell.KeyEnter:
		if p.SelectedIndex < 0 || p.SelectedIndex >= len(p.Items) {
			return true
		}
		idx := p.SelectedIndex
		label := p.Items[idx]
		cb := p.OnPick
		p.Close()
		if cb != nil {
			cb(c, idx, label)
		}
		return true
	case tcell.KeyUp:
		if p.SelectedIndex > 0 {
			p.SelectedIndex--
		}
		return true
	case tcell.KeyDown:
		if p.SelectedIndex < len(p.Items)-1 {
			p.SelectedIndex++
		}
		return true
	}
	return false
}
