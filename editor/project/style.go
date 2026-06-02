// Package project handles on-disk persistence for editor projects.
//
// A project is a directory:
//
//	mygame/
//	  project.toml          ← Project metadata
//	  sprites/*.sprite      ← SpriteFile entries
//	  scenes/*.scene        ← SceneFile entries
//
// All files are TOML. This file (style.go) handles tcell.Style ⇄ TOML
// conversion: scene files and sprite files reference styles by inline
// table. Colors accept named tcell colors, "#rrggbb" hex, or "default".
package project

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// StyleTOML is the on-disk representation of a tcell.Style. All fields are
// optional; an empty StyleTOML decodes to tcell.StyleDefault. Empty Fg/Bg
// strings are treated as "default" so authors can omit colors entirely.
type StyleTOML struct {
	Fg        string `toml:"fg,omitempty"`
	Bg        string `toml:"bg,omitempty"`
	Bold      bool   `toml:"bold,omitempty"`
	Italic    bool   `toml:"italic,omitempty"`
	Underline bool   `toml:"underline,omitempty"`
	Reverse   bool   `toml:"reverse,omitempty"`
}

// EncodeStyle produces a StyleTOML from a tcell.Style. Default/zero styles
// produce a zero-value StyleTOML; empty fields are then omitted by the TOML
// encoder via omitempty so the on-disk file stays minimal.
func EncodeStyle(s tcell.Style) StyleTOML {
	fg, bg, attr := s.Decompose()
	return StyleTOML{
		Fg:        encodeColor(fg),
		Bg:        encodeColor(bg),
		Bold:      attr&tcell.AttrBold != 0,
		Italic:    attr&tcell.AttrItalic != 0,
		Underline: attr&tcell.AttrUnderline != 0,
		Reverse:   attr&tcell.AttrReverse != 0,
	}
}

// DecodeStyle reconstructs a tcell.Style from a StyleTOML. Unknown color
// names produce an error rather than silently falling back to default —
// the editor surfaces the error so the user can fix the file.
func DecodeStyle(t StyleTOML) (tcell.Style, error) {
	s := tcell.StyleDefault
	fg, err := decodeColor(t.Fg)
	if err != nil {
		return s, fmt.Errorf("fg: %w", err)
	}
	bg, err := decodeColor(t.Bg)
	if err != nil {
		return s, fmt.Errorf("bg: %w", err)
	}
	s = s.Foreground(fg).Background(bg)
	if t.Bold {
		s = s.Bold(true)
	}
	if t.Italic {
		s = s.Italic(true)
	}
	if t.Underline {
		s = s.Underline(true)
	}
	if t.Reverse {
		s = s.Reverse(true)
	}
	return s, nil
}

// IsZero reports whether the style is empty enough to be omitted from
// output entirely (saves keep files smaller; default styles need no entry).
func (t StyleTOML) IsZero() bool {
	return t.Fg == "" && t.Bg == "" &&
		!t.Bold && !t.Italic && !t.Underline && !t.Reverse
}

// --- color codec ---------------------------------------------------------

// encodeColor produces the canonical string form for a tcell.Color. Unknown
// or default colors return "" so the encoder omits them.
func encodeColor(c tcell.Color) string {
	if c == tcell.ColorDefault {
		return ""
	}
	if name, ok := colorByValue[c]; ok {
		return name
	}
	if c.IsRGB() {
		r, g, b := c.RGB()
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	// Palette / 256-color: emit as decimal so it round-trips losslessly.
	return strconv.Itoa(int(c) - int(tcell.ColorValid))
}

// decodeColor parses a color string. Empty / "default" → ColorDefault.
// Hex "#rrggbb" → RGB. Named colors → tcell.ColorXxx. Decimal → palette
// index.
func decodeColor(s string) (tcell.Color, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "default" {
		return tcell.ColorDefault, nil
	}
	if strings.HasPrefix(s, "#") {
		if len(s) != 7 {
			return tcell.ColorDefault, fmt.Errorf("hex color must be #rrggbb, got %q", s)
		}
		v, err := strconv.ParseUint(s[1:], 16, 32)
		if err != nil {
			return tcell.ColorDefault, fmt.Errorf("invalid hex color %q: %w", s, err)
		}
		return tcell.NewHexColor(int32(v)), nil
	}
	if c, ok := colorByName[strings.ToLower(s)]; ok {
		return c, nil
	}
	// Decimal palette index fallback.
	if n, err := strconv.Atoi(s); err == nil {
		return tcell.PaletteColor(n), nil
	}
	return tcell.ColorDefault, fmt.Errorf("unknown color %q", s)
}

// colorByName covers the common named colors used by examples and the
// editor's default palette. Extending this map is cheap; tcell.ColorNames
// is exhaustive and we lean on it.
var colorByName = func() map[string]tcell.Color {
	m := make(map[string]tcell.Color, len(tcell.ColorNames))
	for name, c := range tcell.ColorNames {
		m[strings.ToLower(name)] = c
	}
	return m
}()

// colorByValue inverts colorByName. Two names sometimes map to the same
// value (tcell aliases); we keep whichever name comes first via map
// iteration — tests assert canonicalization, so the choice is stable
// enough for round-trips.
var colorByValue = func() map[tcell.Color]string {
	m := make(map[tcell.Color]string, len(tcell.ColorNames))
	for name, c := range tcell.ColorNames {
		if _, exists := m[c]; !exists {
			m[c] = strings.ToLower(name)
		}
	}
	return m
}()
