package editor

import (
	"strings"
	"sync"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/ui"
)

// The Terminal panel runs the user's $SHELL inside a real PTY and renders its
// screen with ImGui's (monospace) default font. Bytes from the shell are read on
// a goroutine into a VT emulator (vtEngine); the panel reads the resulting cell
// grid each frame and forwards keystrokes back to the PTY while focused. The PTY
// and emulation live behind ptySession / vtEngine so this panel is platform- and
// engine-agnostic. v1 has no scrollback (the emulator is screen-only).

type terminal struct {
	mu      sync.Mutex
	vt      vtEngine
	pty     *ptySession
	cols    int
	rows    int
	started bool
	exited  bool  // shell ended or PTY unsupported
	err     error // non-nil when the shell could not start

	// focused mirrors window focus for handleShortcuts, which runs before the
	// panel draws and so reads last frame's value (a one-frame lag is harmless).
	focused bool

	// fontSize is the (unscaled) point size the panel renders at, adjustable with
	// Cmd/Ctrl +/- while focused. UI-goroutine only, so no lock. 0 = default.
	fontSize float32
}

// Terminal font size bounds (unscaled points); DPI scale is applied on top.
const (
	termFontMin = 8
	termFontMax = 40
)

// zoom nudges the font size by delta points, clamped. Cell metrics, grid size,
// and the PTY resize all follow from the new size on the next frame.
func (t *terminal) zoom(delta float32) {
	s := t.fontSize
	if s <= 0 {
		s = termFontSize
	}
	s += delta
	if s < termFontMin {
		s = termFontMin
	}
	if s > termFontMax {
		s = termFontMax
	}
	t.fontSize = s
}

// ensure starts the shell on first use, sized to cols×rows.
func (t *terminal) ensure(cols, rows int) {
	t.mu.Lock()
	if t.started {
		t.mu.Unlock()
		return
	}
	t.started = true
	p, err := startPTY(cols, rows)
	if err != nil {
		t.err = err
		t.exited = true
		t.mu.Unlock()
		return
	}
	vt := newVTEngine(p, cols, rows)
	t.pty = p
	t.vt = vt
	t.cols, t.rows = cols, rows
	t.exited = false
	t.mu.Unlock()

	go t.readLoop(p, vt)
}

// readLoop pumps PTY output into the emulator until the shell exits. vt and p are
// passed in so a later restart (which swaps t.vt/t.pty) never races this loop.
func (t *terminal) readLoop(p *ptySession, vt vtEngine) {
	buf := make([]byte, 32*1024)
	for {
		n, err := p.Read(buf)
		if n > 0 {
			vt.Write(buf[:n]) // vt is internally synchronized
		}
		if err != nil {
			t.mu.Lock()
			if t.pty == p { // still the current session
				t.exited = true
			}
			t.mu.Unlock()
			return
		}
	}
}

// restart kills the current shell and launches a fresh one at cols×rows.
func (t *terminal) restart(cols, rows int) {
	t.kill()
	t.mu.Lock()
	t.started = false
	t.vt = nil
	t.mu.Unlock()
	t.ensure(cols, rows)
}

// kill terminates the shell and releases the PTY. Safe to call repeatedly and
// from the editor exit hook.
func (t *terminal) kill() {
	t.mu.Lock()
	p := t.pty
	t.pty = nil
	t.mu.Unlock()
	if p != nil {
		p.kill()
	}
}

// write sends bytes to the shell (no-op once it has exited).
func (t *terminal) write(b []byte) {
	if len(b) == 0 {
		return
	}
	t.mu.Lock()
	p := t.pty
	t.mu.Unlock()
	if p != nil {
		p.Write(b)
	}
}

// resize matches the emulator and PTY to a new cell grid (delivers SIGWINCH).
func (t *terminal) resize(cols, rows int) {
	t.mu.Lock()
	if !t.started || t.exited || t.pty == nil || (cols == t.cols && rows == t.rows) {
		t.mu.Unlock()
		return
	}
	t.cols, t.rows = cols, rows
	vt, p := t.vt, t.pty
	t.mu.Unlock()
	if vt != nil {
		vt.Resize(cols, rows)
	}
	p.resize(cols, rows)
}

func packU32(v imgui.Vec4) uint32 { return imgui.ColorConvertFloat4ToU32(v) }

// termFontSize is the unscaled base size pushed for the terminal's monospace
// font; DPI scaling is applied on top by the UI's global font scale.
const termFontSize = 14

// drawTerminal renders the Terminal panel.
func drawTerminal(c *app.Ctx, st *state) {
	if st.term == nil {
		st.term = &terminal{fontSize: termFontSize}
	}
	t := st.term
	// NoNavInputs keeps ImGui keyboard nav from eating arrows/Tab/Enter so they
	// reach the shell; NoScrollbar because the emulator owns the screen (v1 has
	// no scrollback).
	if !imgui.BeginV("Terminal", nil, imgui.WindowFlagsNoNavInputs|imgui.WindowFlagsNoScrollbar) {
		t.focused = false
		imgui.End()
		return
	}
	defer imgui.End()

	// Render with the embedded monospace font (box-drawing/braille/symbol glyphs
	// the default ProggyClean font lacks). Deferred after End so PopFont (LIFO)
	// runs before End. Falls back to the default font if the asset is missing.
	size := t.fontSize
	if size <= 0 {
		size = termFontSize
	}
	if mono := ui.MonoFont(c); mono != nil {
		imgui.PushFont(mono, size)
		defer imgui.PopFont()
	}

	// Cell metrics from the (fixed-width) terminal font.
	cellSize := imgui.CalcTextSize("M")
	cellW, cellH := cellSize.X, cellSize.Y
	if cellW < 1 {
		cellW = 7
	}
	if cellH < 1 {
		cellH = 13
	}

	avail := imgui.ContentRegionAvail()
	cols := int(avail.X / cellW)
	rows := int(avail.Y / cellH)
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}

	// Defer starting until the docked panel has a usable size, so the shell never
	// briefly runs at a 1×1 grid.
	if !t.started && cols >= 8 && rows >= 2 {
		t.ensure(cols, rows)
	}

	t.mu.Lock()
	started, exited, startErr := t.started, t.exited, t.err
	t.mu.Unlock()

	t.focused = imgui.IsWindowFocusedV(imgui.FocusedFlagsRootAndChildWindows)

	if !started {
		imgui.TextDisabled("starting shell...")
		return
	}
	if exited {
		if startErr != nil {
			imgui.TextColored(imgui.Vec4{X: 1, Y: 0.4, Z: 0.35, W: 1}, startErr.Error())
		} else {
			imgui.TextDisabled("[process exited] — press Enter to restart")
			if t.focused && (imgui.IsKeyPressedBool(imgui.KeyEnter) || imgui.IsKeyPressedBool(imgui.KeyKeypadEnter)) {
				t.restart(cols, rows)
			}
		}
		return
	}

	t.resize(cols, rows)

	if t.focused {
		t.handleInput(c)
	}

	// An invisible button over the grid makes clicks focus the panel and reserves
	// the layout space; the grid itself is painted with the window draw list.
	origin := imgui.CursorScreenPos()
	imgui.InvisibleButton("##term-grid", imgui.Vec2{X: float32(cols) * cellW, Y: float32(rows) * cellH})

	dl := imgui.WindowDrawList()
	gridMax := imgui.Vec2{X: origin.X + float32(cols)*cellW, Y: origin.Y + float32(rows)*cellH}
	dl.AddRectFilled(origin, gridMax, packU32(termDefaultBG))

	t.mu.Lock()
	vt := t.vt
	t.mu.Unlock()
	if vt == nil {
		return
	}

	vt.Lock()
	vcols, vrows := vt.Size()
	if vcols > cols {
		vcols = cols
	}
	if vrows > rows {
		vrows = rows
	}
	for y := 0; y < vrows; y++ {
		rowY := origin.Y + float32(y)*cellH
		// Backgrounds: fill contiguous runs of the same non-default color.
		for x := 0; x < vcols; {
			cell := vt.Cell(x, y)
			if cell.bg == termDefaultBG {
				x++
				continue
			}
			x2 := x + 1
			for x2 < vcols && vt.Cell(x2, y).bg == cell.bg {
				x2++
			}
			dl.AddRectFilled(
				imgui.Vec2{X: origin.X + float32(x)*cellW, Y: rowY},
				imgui.Vec2{X: origin.X + float32(x2)*cellW, Y: rowY + cellH},
				packU32(cell.bg))
			x = x2
		}
		// Glyphs: one AddText per run of identical foreground color.
		for x := 0; x < vcols; {
			fg := vt.Cell(x, y).fg
			startX := x
			var sb strings.Builder
			for x < vcols {
				cell := vt.Cell(x, y)
				if cell.fg != fg {
					break
				}
				r := cell.r
				if r == 0 {
					r = ' '
				}
				sb.WriteRune(r)
				x++
			}
			if s := strings.TrimRight(sb.String(), " "); s != "" {
				dl.AddTextVec2(imgui.Vec2{X: origin.X + float32(startX)*cellW, Y: rowY}, packU32(fg), s)
			}
		}
	}
	cx, cy, cvis := vt.Cursor()
	vt.Unlock()

	// Block cursor (semi-transparent so the glyph underneath stays legible).
	if cvis && cx >= 0 && cx < cols && cy >= 0 && cy < rows {
		col := termDefaultFG
		col.W = 0.55
		if !t.focused {
			col.W = 0.25
		}
		pmin := imgui.Vec2{X: origin.X + float32(cx)*cellW, Y: origin.Y + float32(cy)*cellH}
		pmax := imgui.Vec2{X: pmin.X + cellW, Y: pmin.Y + cellH}
		dl.AddRectFilled(pmin, pmax, packU32(col))
	}
}

// handleInput forwards this frame's keystrokes to the shell while the panel is
// focused. Printable text comes from ui.FrameRunes; control/navigation keys are
// translated to their terminal byte sequences.
func (t *terminal) handleInput(c *app.Ctx) {
	io := imgui.CurrentIO()
	ctrl := io.KeyCtrl()
	cmd := io.KeySuper()

	// Font zoom: Cmd +/- on macOS, Ctrl +/- elsewhere (Cmd 0 / Ctrl 0 resets).
	// '+' shares the '=' key, so KeyEqual covers both. Handled here so the chord
	// never reaches the shell.
	if cmd || ctrl {
		switch {
		case imgui.IsKeyPressedBool(imgui.KeyEqual) || imgui.IsKeyPressedBool(imgui.KeyKeypadAdd):
			t.zoom(+1)
			return
		case imgui.IsKeyPressedBool(imgui.KeyMinus) || imgui.IsKeyPressedBool(imgui.KeyKeypadSubtract):
			t.zoom(-1)
			return
		case imgui.IsKeyPressedBool(imgui.Key0) || imgui.IsKeyPressedBool(imgui.KeyKeypad0):
			t.fontSize = termFontSize
			return
		}
	}

	var out []byte

	// Paste: Cmd+V (macOS) or Ctrl+V.
	if (cmd || ctrl) && imgui.IsKeyPressedBool(imgui.KeyV) {
		t.write([]byte(imgui.ClipboardText()))
		return
	}

	// Printable text, unless a Ctrl/Cmd chord is held (those are commands, and
	// the platform usually emits no character for them anyway).
	if !ctrl && !cmd {
		for _, r := range ui.FrameRunes(c) {
			out = append(out, []byte(string(r))...)
		}
	}

	// Ctrl+letter → control byte (Ctrl+C = 0x03, Ctrl+D = 0x04, ...).
	if ctrl {
		for k := imgui.KeyA; k <= imgui.KeyZ; k++ {
			if imgui.IsKeyPressedBool(k) {
				out = append(out, byte(k-imgui.KeyA)+1)
			}
		}
	}

	for _, m := range keySeqs {
		if imgui.IsKeyPressedBool(m.key) {
			out = append(out, m.seq...)
		}
	}

	t.write(out)
}

// keySeqs maps navigation/editing keys to the byte sequences an xterm expects.
var keySeqs = []struct {
	key imgui.Key
	seq []byte
}{
	{imgui.KeyEnter, []byte("\r")},
	{imgui.KeyKeypadEnter, []byte("\r")},
	{imgui.KeyBackspace, []byte{0x7f}},
	{imgui.KeyTab, []byte("\t")},
	{imgui.KeyEscape, []byte{0x1b}},
	{imgui.KeyUpArrow, []byte("\x1b[A")},
	{imgui.KeyDownArrow, []byte("\x1b[B")},
	{imgui.KeyRightArrow, []byte("\x1b[C")},
	{imgui.KeyLeftArrow, []byte("\x1b[D")},
	{imgui.KeyHome, []byte("\x1b[H")},
	{imgui.KeyEnd, []byte("\x1b[F")},
	{imgui.KeyDelete, []byte("\x1b[3~")},
	{imgui.KeyPageUp, []byte("\x1b[5~")},
	{imgui.KeyPageDown, []byte("\x1b[6~")},
}
