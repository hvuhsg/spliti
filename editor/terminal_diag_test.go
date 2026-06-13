package editor

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

// TestDiagRichTUI replays Claude-Code-style terminal output through the vt10x
// engine and dumps the resulting cell grid so we can see whether the EMULATOR
// models a rich TUI correctly (separate from whether the font can draw the
// glyphs). Run: go test ./editor -run TestDiagRichTUI -v
func TestDiagRichTUI(t *testing.T) {
	const cols, rows = 40, 8
	vt := newVTEngine(io.Discard, cols, rows)

	// A rounded box with a truecolor border, a braille spinner, a colored label,
	// then a cursor jump + erase-line + rewrite (the live-update pattern).
	seq := "" +
		"\x1b[1;1H" + // cursor home
		"\x1b[38;2;120;170;255m" + // truecolor border (bluish)
		"╭──────────────────────────╮\r\n" +
		"│ \x1b[0m\x1b[1;92m⠋ Working…\x1b[0m\x1b[38;2;120;170;255m            │\r\n" +
		"╰──────────────────────────╯\x1b[0m\r\n" +
		"\x1b[5;1Hold junk text here xxxxxxxxx" + // line 5 filled
		"\x1b[5;1H\x1b[K" + // jump back to line 5 col 1, erase line
		"\x1b[38;2;255;120;0mrewritten ✓\x1b[0m" // new content

	if _, err := vt.Write([]byte(seq)); err != nil {
		t.Fatalf("write: %v", err)
	}

	vt.Lock()
	defer vt.Unlock()

	var sb strings.Builder
	sb.WriteString("\n--- grid dump (rune per cell) ---\n")
	for y := 0; y < rows; y++ {
		sb.WriteByte('|')
		for x := 0; x < cols; x++ {
			r := vt.Cell(x, y).r
			if r == 0 {
				r = ' '
			}
			sb.WriteRune(r)
		}
		sb.WriteString("|\n")
	}
	t.Log(sb.String())

	// Spot-check coherence (emulator-level correctness, not glyph drawing).
	checks := []struct {
		x, y int
		want rune
		desc string
	}{
		{0, 0, '╭', "top-left rounded corner preserved as a rune"},
		{27, 0, '╮', "top-right corner at expected column"},
		{0, 2, '╰', "bottom-left corner"},
		{2, 1, '⠋', "braille spinner rune preserved"},
	}
	for _, c := range checks {
		if got := vt.Cell(c.x, c.y).r; got != c.want {
			t.Errorf("cell(%d,%d)=%q want %q — %s", c.x, c.y, got, c.want, c.desc)
		} else {
			t.Logf("OK cell(%d,%d)=%q — %s", c.x, c.y, got, c.desc)
		}
	}

	// Truecolor border color should resolve to the bluish RGB we set.
	border := vt.Cell(0, 0).fg
	wantBorder := rgb(120, 170, 255)
	t.Logf("border fg = %+v (want %+v)", border, wantBorder)
	if border != wantBorder {
		t.Errorf("border truecolor not applied: got %+v want %+v", border, wantBorder)
	}

	// Erase-line + rewrite: line 4 (index 4) should start with "rewritten" and
	// NOT contain the old "junk".
	var line4 strings.Builder
	for x := 0; x < cols; x++ {
		r := vt.Cell(x, 4).r
		if r == 0 {
			r = ' '
		}
		line4.WriteRune(r)
	}
	l4 := line4.String()
	t.Logf("line4 = %q", l4)
	if strings.Contains(l4, "junk") {
		t.Errorf("erase-line failed: old 'junk' still present on line 4: %q", l4)
	}
	if !strings.HasPrefix(strings.TrimSpace(l4), "rewritten") {
		t.Errorf("rewrite failed: line 4 = %q", l4)
	}

	fmt.Println("diag complete")
}
