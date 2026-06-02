package ui

import (
	"github.com/gdamore/tcell/v2"
)

// Style palette for editor chrome. Centralised here so panels render with
// a consistent look. Colors are conservative for compatibility with
// limited palettes — distinct enough to read, plain enough to not fight
// the user's terminal theme.
var (
	StyleChrome        = tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	StyleChromeFocused = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	StyleTitle         = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	StyleText          = tcell.StyleDefault.Foreground(tcell.ColorSilver)
	StyleTextDim       = tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	StyleSelected      = tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorYellow)
	StyleHotKey        = tcell.StyleDefault.Foreground(tcell.ColorAqua).Bold(true)
)

// DrawText writes text starting at (x,y) using style. No clipping; callers
// pass rects that fit. Tab and newline characters are written verbatim,
// which is fine for terminal output but caller-controlled.
func DrawText(s tcell.Screen, x, y int, style tcell.Style, text string) {
	for i, r := range text {
		s.SetContent(x+i, y, r, nil, style)
	}
}

// DrawTextClipped is like DrawText but stops at maxWidth runes (not bytes).
func DrawTextClipped(s tcell.Screen, x, y, maxWidth int, style tcell.Style, text string) {
	if maxWidth <= 0 {
		return
	}
	i := 0
	for _, r := range text {
		if i >= maxWidth {
			return
		}
		s.SetContent(x+i, y, r, nil, style)
		i++
	}
}

// DrawHLine draws a horizontal line of length n starting at (x,y) using ch.
func DrawHLine(s tcell.Screen, x, y, n int, ch rune, style tcell.Style) {
	for i := 0; i < n; i++ {
		s.SetContent(x+i, y, ch, nil, style)
	}
}

// DrawVLine draws a vertical line of length n starting at (x,y) using ch.
func DrawVLine(s tcell.Screen, x, y, n int, ch rune, style tcell.Style) {
	for i := 0; i < n; i++ {
		s.SetContent(x, y+i, ch, nil, style)
	}
}

// DrawFill fills r with ch+style. Used to clear panel interiors.
func DrawFill(s tcell.Screen, r Rect, ch rune, style tcell.Style) {
	for y := 0; y < r.H; y++ {
		for x := 0; x < r.W; x++ {
			s.SetContent(r.X+x, r.Y+y, ch, nil, style)
		}
	}
}

// DrawBox draws a Unicode box-drawing border around r and writes title in
// the top edge. focused selects between dim and bright chrome styles. The
// interior is NOT cleared — call DrawFill first if needed.
//
// Boxes smaller than 2x2 degrade gracefully: a 1xN draws nothing, a 2x2
// draws just corners, etc. Title is clipped to the available width.
func DrawBox(s tcell.Screen, r Rect, title string, focused bool) {
	if r.W < 2 || r.H < 2 {
		return
	}
	style := StyleChrome
	if focused {
		style = StyleChromeFocused
	}
	// Corners.
	s.SetContent(r.X, r.Y, '┌', nil, style)
	s.SetContent(r.X+r.W-1, r.Y, '┐', nil, style)
	s.SetContent(r.X, r.Y+r.H-1, '└', nil, style)
	s.SetContent(r.X+r.W-1, r.Y+r.H-1, '┘', nil, style)
	// Edges.
	DrawHLine(s, r.X+1, r.Y, r.W-2, '─', style)
	DrawHLine(s, r.X+1, r.Y+r.H-1, r.W-2, '─', style)
	DrawVLine(s, r.X, r.Y+1, r.H-2, '│', style)
	DrawVLine(s, r.X+r.W-1, r.Y+1, r.H-2, '│', style)
	// Title in top edge: "─ Title ─".
	if title != "" && r.W >= 6 {
		max := r.W - 4
		t := title
		if len(t) > max {
			t = t[:max]
		}
		startX := r.X + 2
		s.SetContent(startX-1, r.Y, ' ', nil, style)
		ts := StyleTitle
		if focused {
			ts = ts.Foreground(tcell.ColorYellow)
		}
		DrawText(s, startX, r.Y, ts, t)
		s.SetContent(startX+len(t), r.Y, ' ', nil, style)
	}
}
