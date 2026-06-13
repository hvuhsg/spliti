package editor

import (
	"io"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hinshun/vt10x"
)

// vtEngine is the terminal-emulation core behind the Terminal panel: it consumes
// the bytes a PTY produces (Write) and exposes the resulting screen as a grid of
// cells plus a cursor. The panel and PTY code talk only to this interface, so the
// engine can be swapped (e.g. for mitchellh/go-libghostty's libghostty-vt) by
// adding another implementation and changing newVTEngine — nothing else moves.
type vtEngine interface {
	// Write feeds PTY output bytes into the emulator.
	io.Writer
	// Resize changes the screen dimensions (call alongside the PTY's own resize).
	Resize(cols, rows int)
	// Size returns the current screen dimensions.
	Size() (cols, rows int)
	// Cursor returns the cursor cell and whether it is visible.
	Cursor() (col, row int, visible bool)
	// Cell returns the resolved glyph at (col,row). Must be called under Lock.
	Cell(col, row int) vtCell
	// Lock/Unlock guard a consistent snapshot read against the reader goroutine.
	Lock()
	Unlock()
}

// vtCell is one screen cell with colors already resolved to RGBA for drawing.
type vtCell struct {
	r      rune
	fg, bg imgui.Vec4
}

// newVTEngine builds the default (pure-Go) engine. responder receives the
// terminal's replies to device queries (cursor-position reports, etc.) and is
// normally the PTY master so the shell sees them.
func newVTEngine(responder io.Writer, cols, rows int) vtEngine {
	return &vt10xEngine{t: vt10x.New(vt10x.WithWriter(responder), vt10x.WithSize(cols, rows))}
}

// vt10xEngine adapts github.com/hinshun/vt10x to vtEngine. This file is the only
// place that imports vt10x.
type vt10xEngine struct {
	t vt10x.Terminal
}

func (e *vt10xEngine) Write(p []byte) (int, error) { return e.t.Write(p) }
func (e *vt10xEngine) Resize(cols, rows int)       { e.t.Resize(cols, rows) }
func (e *vt10xEngine) Size() (int, int)            { return e.t.Size() }
func (e *vt10xEngine) Lock()                       { e.t.Lock() }
func (e *vt10xEngine) Unlock()                     { e.t.Unlock() }

func (e *vt10xEngine) Cursor() (int, int, bool) {
	cur := e.t.Cursor()
	return cur.X, cur.Y, e.t.CursorVisible()
}

func (e *vt10xEngine) Cell(col, row int) vtCell {
	g := e.t.Cell(col, row)
	fg, bg := resolveColors(g)
	return vtCell{r: g.Char, fg: fg, bg: bg}
}

// vt10x's glyph attribute bits are unexported; these mirror the bit positions
// declared in vt10x/state.go (attrReverse = 1<<0, attrBold = 1<<2).
const (
	vtAttrReverse = 1 << 0
	vtAttrBold    = 1 << 2
)

// termDefaultFG / termDefaultBG are used for vt10x's default fg/bg sentinels and
// for the panel's backdrop. They sit in the same 0..1 space the rest of the UI
// uses for style colors.
var (
	termDefaultFG = imgui.Vec4{X: 0.85, Y: 0.85, Z: 0.85, W: 1}
	termDefaultBG = imgui.Vec4{X: 0.09, Y: 0.09, Z: 0.11, W: 1}
)

func resolveColors(g vt10x.Glyph) (fg, bg imgui.Vec4) {
	fgc, bgc := g.FG, g.BG
	// Bold brightens the standard 8 ANSI foregrounds to their bright variants.
	if g.Mode&vtAttrBold != 0 && fgc < 8 {
		fgc += 8
	}
	fg = resolveColor(fgc)
	bg = resolveColor(bgc)
	if g.Mode&vtAttrReverse != 0 {
		fg, bg = bg, fg
	}
	return fg, bg
}

func resolveColor(c vt10x.Color) imgui.Vec4 {
	switch {
	case c == vt10x.DefaultFG || c == vt10x.DefaultCursor:
		return termDefaultFG
	case c == vt10x.DefaultBG:
		return termDefaultBG
	case c < 16:
		return ansiPalette[c]
	case c < 256:
		return xterm256(uint32(c))
	default: // 24-bit truecolor packed as r<<16|g<<8|b
		return rgb(uint8(c>>16), uint8(c>>8), uint8(c))
	}
}

func rgb(r, g, b uint8) imgui.Vec4 {
	return imgui.Vec4{X: float32(r) / 255, Y: float32(g) / 255, Z: float32(b) / 255, W: 1}
}

// ansiPalette is the standard 16-color ANSI table (normal 0..7, bright 8..15).
var ansiPalette = [16]imgui.Vec4{
	rgb(0, 0, 0), rgb(205, 49, 49), rgb(13, 188, 121), rgb(229, 229, 16),
	rgb(36, 114, 200), rgb(188, 63, 188), rgb(17, 168, 205), rgb(190, 190, 190),
	rgb(102, 102, 102), rgb(241, 76, 76), rgb(35, 209, 139), rgb(245, 245, 67),
	rgb(59, 142, 234), rgb(214, 112, 214), rgb(41, 184, 219), rgb(255, 255, 255),
}

// xterm256 resolves the xterm 256-color palette entries 16..255 (6x6x6 cube and
// the 24-step grayscale ramp).
func xterm256(c uint32) imgui.Vec4 {
	if c < 232 {
		i := c - 16
		comp := func(v uint32) uint8 {
			if v == 0 {
				return 0
			}
			return uint8(55 + v*40)
		}
		return rgb(comp(i/36), comp((i/6)%6), comp(i%6))
	}
	g := uint8(8 + (c-232)*10)
	return rgb(g, g, g)
}
