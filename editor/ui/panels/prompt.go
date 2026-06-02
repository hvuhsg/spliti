package panels

import (
	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/tui"
)

// Prompt is a modal text-input overlay. While Active is true, the
// editor's mouse and key routers short-circuit every event into this
// panel — no other panel receives input. On Enter, OnSubmit is called
// with the buffer; on Esc, OnCancel runs (or the prompt simply closes).
//
// Prompt is implemented as a panel for symmetry with the other UI
// (Render/OnKey) but it is NOT registered in the layout's named-slot
// table. The editor places it manually at a centered rect each frame.
//
// Multi-step input is handled by chaining prompts: OnSubmit can call a
// helper that opens the next prompt with new state. The Prompt itself
// is stateless across submits — it's just a single-line text field.
type Prompt struct {
	Active    bool
	TitleText string
	Hint      string
	Buffer    string
	OnSubmit  func(c *app.Ctx, value string)
	OnCancel  func(c *app.Ctx)
}

// Name implements ui.Panel.
func (*Prompt) Name() string { return "prompt" }

// Title implements ui.Panel.
func (p *Prompt) Title() string { return p.TitleText }

// Open initialises and shows the prompt with a clean buffer.
func (p *Prompt) Open(title, hint string, onSubmit func(c *app.Ctx, value string), onCancel func(c *app.Ctx)) {
	p.TitleText = title
	p.Hint = hint
	p.Buffer = ""
	p.OnSubmit = onSubmit
	p.OnCancel = onCancel
	p.Active = true
}

// Close hides the prompt and clears callbacks.
func (p *Prompt) Close() {
	p.Active = false
	p.Buffer = ""
	p.OnSubmit = nil
	p.OnCancel = nil
}

// Render draws the prompt at the centered Rect computed by layout. When
// inactive, draws nothing. The caller (editor render system) computes
// the rect based on screen size.
func (p *Prompt) Render(c *app.Ctx, r ui.Rect, _ bool) {
	if !p.Active {
		return
	}
	s := tui.Screen(c)
	if s == nil {
		return
	}
	// Solid background for modal effect.
	bg := tcell.StyleDefault.Background(tcell.ColorBlack)
	ui.DrawFill(s, r, ' ', bg)
	ui.DrawBox(s, r, p.TitleText, true)

	inner := r.Inset(1)
	if inner.W <= 0 || inner.H <= 0 {
		return
	}
	if p.Hint != "" {
		ui.DrawTextClipped(s, inner.X, inner.Y, inner.W, ui.StyleTextDim, p.Hint)
	}
	// Input row.
	inputY := inner.Y + 2
	if inputY < inner.Y+inner.H {
		ui.DrawText(s, inner.X, inputY, ui.StyleText, "› ")
		ui.DrawTextClipped(s, inner.X+2, inputY, inner.W-2, ui.StyleTitle, p.Buffer+"_")
	}
	// Footer hint.
	footY := inner.Y + inner.H - 1
	if footY > inputY {
		ui.DrawTextClipped(s, inner.X, footY, inner.W, ui.StyleTextDim, "Enter: confirm   Esc: cancel")
	}
}

// OnMouse implements ui.Panel. Modal: ignore clicks outside; nothing to
// do for clicks inside (no buttons in v1).
func (p *Prompt) OnMouse(_ *app.Ctx, _, _ int, _ input.MouseEvent) {}

// OnKey implements ui.Panel. Backspace deletes; Esc cancels; Enter
// submits; printable runes append.
func (p *Prompt) OnKey(c *app.Ctx, ev input.KeyEvent) bool {
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
		val := p.Buffer
		cb := p.OnSubmit
		p.Close()
		if cb != nil {
			cb(c, val)
		}
		return true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(p.Buffer) > 0 {
			rs := []rune(p.Buffer)
			p.Buffer = string(rs[:len(rs)-1])
		}
		return true
	}
	if ev.Rune >= 32 {
		p.Buffer += string(ev.Rune)
		return true
	}
	return false
}

// PromptRect computes a centered rect of (w, h) within the screen of
// (sw, sh). Caller passes sane sizes; we don't clamp.
func PromptRect(sw, sh, w, h int) ui.Rect {
	if w > sw {
		w = sw
	}
	if h > sh {
		h = sh
	}
	return ui.Rect{X: (sw - w) / 2, Y: (sh - h) / 2, W: w, H: h}
}
