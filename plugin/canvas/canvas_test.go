package canvas_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/plugin/canvas"
)

func TestSetBounds(t *testing.T) {
	c := canvas.New(4, 2) // 4 x 4 pixels
	// In-bounds write sticks.
	c.Set(1, 3, canvas.RGB{R: 10, G: 20, B: 30})
	if got := c.Pix[3*4+1]; got != (canvas.RGB{R: 10, G: 20, B: 30}) {
		t.Fatalf("Set in-bounds = %+v", got)
	}
	// Out-of-bounds writes must not panic and must not corrupt the buffer.
	c.Set(-1, 0, canvas.RGB{R: 1})
	c.Set(0, -1, canvas.RGB{R: 1})
	c.Set(4, 0, canvas.RGB{R: 1})
	c.Set(0, 4, canvas.RGB{R: 1})
	for i, p := range c.Pix {
		if i == 3*4+1 {
			continue
		}
		if p != (canvas.RGB{}) {
			t.Fatalf("OOB Set leaked into Pix[%d]=%+v", i, p)
		}
	}
}

func TestVLineClamps(t *testing.T) {
	c := canvas.New(2, 2) // 2 x 4 pixels
	// Span overflows both ends; should clamp to [0,3].
	c.VLine(0, -5, 100, canvas.RGB{R: 99})
	for y := 0; y < 4; y++ {
		if got := c.Pix[y*2+0]; got != (canvas.RGB{R: 99}) {
			t.Fatalf("VLine col 0 row %d = %+v", y, got)
		}
	}
	// Column 1 untouched.
	for y := 0; y < 4; y++ {
		if got := c.Pix[y*2+1]; got != (canvas.RGB{}) {
			t.Fatalf("VLine bled into col 1 row %d = %+v", y, got)
		}
	}
	// Out-of-range column is a no-op.
	c.VLine(5, 0, 3, canvas.RGB{R: 1})
}

func TestBlitTopBottomMapping(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatalf("sim init: %v", err)
	}
	s.SetSize(4, 4)

	c := canvas.New(2, 1) // 2 x 2 pixels -> one cell row
	top := canvas.RGB{R: 255, G: 0, B: 0}
	bot := canvas.RGB{R: 0, G: 0, B: 255}
	c.Set(0, 0, top) // top pixel of cell (0,0)
	c.Set(0, 1, bot) // bottom pixel of cell (0,0)

	c.Blit(s, 1, 1) // offset by one cell
	s.Show()        // flush back buffer so GetContents sees it

	cells, _, _ := s.GetContents()
	w, _ := s.Size()
	cell := cells[1*w+1]
	if len(cell.Runes) == 0 || cell.Runes[0] != '▀' {
		t.Fatalf("expected upper-half-block at (1,1), got %q", cell.Runes)
	}
	fg, bg, _ := cell.Style.Decompose()
	wantFg := tcell.NewRGBColor(255, 0, 0)
	wantBg := tcell.NewRGBColor(0, 0, 255)
	if fg != wantFg {
		t.Fatalf("fg = %v, want top pixel %v", fg, wantFg)
	}
	if bg != wantBg {
		t.Fatalf("bg = %v, want bottom pixel %v", bg, wantBg)
	}
}
