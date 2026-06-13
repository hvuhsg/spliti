package editor

import (
	"io"
	"testing"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hinshun/vt10x"
)

// TestVTEngineGrid checks that bytes fed to the engine land in the expected
// cells with the default foreground.
func TestVTEngineGrid(t *testing.T) {
	vt := newVTEngine(io.Discard, 80, 24)
	if _, err := vt.Write([]byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if cols, rows := vt.Size(); cols != 80 || rows != 24 {
		t.Fatalf("size = %dx%d, want 80x24", cols, rows)
	}
	vt.Lock()
	defer vt.Unlock()
	if got := vt.Cell(0, 0).r; got != 'h' {
		t.Errorf("cell(0,0) = %q, want 'h'", got)
	}
	if got := vt.Cell(1, 0).r; got != 'i' {
		t.Errorf("cell(1,0) = %q, want 'i'", got)
	}
	if got := vt.Cell(0, 0).fg; got != termDefaultFG {
		t.Errorf("cell(0,0) fg = %v, want default %v", got, termDefaultFG)
	}
}

// TestVTEngineColor checks that an SGR red sequence colors the following cell.
func TestVTEngineColor(t *testing.T) {
	vt := newVTEngine(io.Discard, 80, 24)
	vt.Write([]byte("\x1b[31mX")) // set fg = ANSI red, then 'X'
	vt.Lock()
	defer vt.Unlock()
	cell := vt.Cell(0, 0)
	if cell.r != 'X' {
		t.Fatalf("cell(0,0) = %q, want 'X'", cell.r)
	}
	if cell.fg != ansiPalette[1] {
		t.Errorf("red fg = %v, want %v", cell.fg, ansiPalette[1])
	}
}

func TestResolveColor(t *testing.T) {
	cases := []struct {
		name string
		in   uint32
		want imgui.Vec4
	}{
		{"ansi-green", 2, ansiPalette[2]},
		{"cube-black", 16, rgb(0, 0, 0)},
		{"cube-white", 231, rgb(255, 255, 255)},
		{"grayscale-first", 232, rgb(8, 8, 8)},
		{"grayscale-last", 255, rgb(238, 238, 238)},
		{"truecolor", 0x336699, rgb(0x33, 0x66, 0x99)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// vt10x.Color is uint32; values <16 hit the ANSI table, the rest the
			// 256/truecolor paths.
			if got := resolveColor(vt10x.Color(tc.in)); got != tc.want {
				t.Errorf("resolveColor(%d) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestKeySeqs guards the navigation byte sequences against accidental edits.
func TestKeySeqs(t *testing.T) {
	want := map[imgui.Key]string{
		imgui.KeyEnter:      "\r",
		imgui.KeyUpArrow:    "\x1b[A",
		imgui.KeyDownArrow:  "\x1b[B",
		imgui.KeyRightArrow: "\x1b[C",
		imgui.KeyLeftArrow:  "\x1b[D",
		imgui.KeyDelete:     "\x1b[3~",
	}
	got := map[imgui.Key]string{}
	for _, m := range keySeqs {
		if _, seen := got[m.key]; !seen { // first wins (KeypadEnter aliases Enter)
			got[m.key] = string(m.seq)
		}
	}
	for k, seq := range want {
		if got[k] != seq {
			t.Errorf("key %v seq = %q, want %q", k, got[k], seq)
		}
	}
	if string([]byte{0x7f}) != string(keySeqByKey(imgui.KeyBackspace)) {
		t.Errorf("backspace should be 0x7f")
	}
}

func TestTerminalZoom(t *testing.T) {
	tm := &terminal{fontSize: termFontSize}

	tm.zoom(+1)
	if tm.fontSize != termFontSize+1 {
		t.Errorf("after +1: got %v want %v", tm.fontSize, termFontSize+1)
	}

	for i := 0; i < 200; i++ {
		tm.zoom(+1)
	}
	if tm.fontSize != termFontMax {
		t.Errorf("zoom in clamps to %v, got %v", float32(termFontMax), tm.fontSize)
	}

	for i := 0; i < 200; i++ {
		tm.zoom(-1)
	}
	if tm.fontSize != termFontMin {
		t.Errorf("zoom out clamps to %v, got %v", float32(termFontMin), tm.fontSize)
	}

	// An unset (zero) size resolves to the default before applying the delta.
	tm2 := &terminal{}
	tm2.zoom(-1)
	if tm2.fontSize != termFontSize-1 {
		t.Errorf("zoom from unset: got %v want %v", tm2.fontSize, termFontSize-1)
	}
}

func keySeqByKey(k imgui.Key) []byte {
	for _, m := range keySeqs {
		if m.key == k {
			return m.seq
		}
	}
	return nil
}
