package panels

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/ui"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/sprite"
	"github.com/hvuhsg/spliti/plugin/tui"
)

// SpriteEdit is the optional bottom strip for editing a sprite asset.
// It's hidden when Layout.SpriteEditorOpen is false; opening an asset
// in the Assets panel sets that flag.
//
// State held on the struct: which Ref is being edited, the brush rune
// and style, the cursor cell, and an in-progress dirty flag the asset
// browser checks before writing back to disk (Phase G).
type SpriteEdit struct {
	Ref       string
	Cursor    struct{ X, Y int }
	Brush     sprite.Cell
	Dirty     bool
}

// Name implements ui.Panel.
func (e *SpriteEdit) Name() string { return ui.NameSpriteEdit }

// Title implements ui.Panel.
func (e *SpriteEdit) Title() string { return "Sprite Editor" }

// Render implements ui.Panel.
func (e *SpriteEdit) Render(c *app.Ctx, r ui.Rect, focused bool) {
	if r.W == 0 || r.H == 0 {
		return // not open
	}
	s := tui.Screen(c)
	if s == nil {
		return
	}
	ui.DrawFill(s, r, ' ', tcell.StyleDefault)
	title := e.Title()
	if e.Ref != "" {
		title += " — " + e.Ref
	}
	ui.DrawBox(s, r, title, focused)

	inner := r.Inset(1)
	if inner.W <= 0 || inner.H <= 0 {
		return
	}

	if e.Ref == "" {
		ui.DrawTextClipped(s, inner.X, inner.Y, inner.W, ui.StyleTextDim, "(no sprite open — pick one in Assets)")
		return
	}
	registry := app.GetResource[sprite.SpriteRegistry](c)
	if registry == nil {
		ui.DrawTextClipped(s, inner.X, inner.Y, inner.W, ui.StyleTextDim, "(no registry)")
		return
	}
	asset := registry.Get(e.Ref)
	if asset == nil {
		ui.DrawTextClipped(s, inner.X, inner.Y, inner.W, ui.StyleTextDim, fmt.Sprintf("(unknown ref %q)", e.Ref))
		return
	}

	// Paint canvas in left half of inner rect.
	canvasX, canvasY := inner.X+1, inner.Y+1
	for y := 0; y < asset.H; y++ {
		for x := 0; x < asset.W; x++ {
			cell := asset.Cells[y*asset.W+x]
			ch := cell.Char
			style := cell.Style
			if cell.Empty {
				ch = '·'
				style = ui.StyleTextDim
			}
			if x == e.Cursor.X && y == e.Cursor.Y {
				style = style.Reverse(true)
			}
			cx := canvasX + x
			cy := canvasY + y
			if cx >= inner.X+inner.W || cy >= inner.Y+inner.H {
				continue
			}
			s.SetContent(cx, cy, ch, nil, style)
		}
	}

	// Right side: brush + hint.
	hintX := inner.X + asset.W + 4
	if hintX < inner.X+inner.W {
		brush := e.Brush.Char
		if brush == 0 {
			brush = '?'
		}
		ui.DrawTextClipped(s, hintX, inner.Y, inner.W-(hintX-inner.X), ui.StyleText,
			fmt.Sprintf("Brush: %c", brush))
		ui.DrawTextClipped(s, hintX, inner.Y+1, inner.W-(hintX-inner.X), ui.StyleTextDim,
			"hjkl/arrows: move • a-z: paint • space: erase • s: save")
	}
}

// OnMouse implements ui.Panel. Click positions the cursor; while
// pressed, paints with the current brush.
func (e *SpriteEdit) OnMouse(c *app.Ctx, lx, ly int, ev input.MouseEvent) {
	if ev.Buttons&tcell.Button1 == 0 {
		return
	}
	registry := app.GetResource[sprite.SpriteRegistry](c)
	if registry == nil {
		return
	}
	asset := registry.Get(e.Ref)
	if asset == nil {
		return
	}
	cx, cy := lx-2, ly-2 // border + 1-cell padding
	if cx < 0 || cy < 0 || cx >= asset.W || cy >= asset.H {
		return
	}
	e.Cursor.X, e.Cursor.Y = cx, cy
	if e.Brush.Char != 0 {
		paint(asset, cx, cy, e.Brush)
		e.Dirty = true
	}
}

// OnKey implements ui.Panel. Movement, painting, eraser, save.
func (e *SpriteEdit) OnKey(c *app.Ctx, ev input.KeyEvent) bool {
	registry := app.GetResource[sprite.SpriteRegistry](c)
	if registry == nil {
		return false
	}
	asset := registry.Get(e.Ref)
	if asset == nil {
		return false
	}
	moved := false
	switch ev.Key {
	case tcell.KeyLeft:
		if e.Cursor.X > 0 {
			e.Cursor.X--
		}
		moved = true
	case tcell.KeyRight:
		if e.Cursor.X < asset.W-1 {
			e.Cursor.X++
		}
		moved = true
	case tcell.KeyUp:
		if e.Cursor.Y > 0 {
			e.Cursor.Y--
		}
		moved = true
	case tcell.KeyDown:
		if e.Cursor.Y < asset.H-1 {
			e.Cursor.Y++
		}
		moved = true
	}
	if moved {
		return true
	}
	if ev.Key == tcell.KeyCtrlS {
		// Save: writes the in-memory asset back to disk through the
		// editor's SpriteSaveAction resource so editor.go can do the
		// actual filesystem write (the panel doesn't know the project
		// directory).
		req := app.GetResource[SpriteSaveAction](c)
		if req != nil {
			req.Ref = e.Ref
		}
		return true
	}
	switch {
	case ev.Rune == ' ':
		// Erase = mark current cell empty.
		paint(asset, e.Cursor.X, e.Cursor.Y, sprite.Cell{Empty: true})
		e.Dirty = true
		return true
	case ev.Rune != 0 && ev.Rune >= 32:
		e.Brush = sprite.Cell{Char: ev.Rune, Style: tcell.StyleDefault}
		paint(asset, e.Cursor.X, e.Cursor.Y, e.Brush)
		e.Dirty = true
		return true
	}
	return false
}

// SpriteSaveAction is the cross-panel signal used by SpriteEdit to ask
// editor.go to write the sprite asset to disk. Editor's apply system
// reads & clears Ref each frame; "" means no pending save.
type SpriteSaveAction struct {
	Ref string
}

// paint writes cell into asset at (x,y). Bounds-checked by callers.
func paint(asset *sprite.SpriteAsset, x, y int, cell sprite.Cell) {
	asset.Cells[y*asset.W+x] = cell
}
