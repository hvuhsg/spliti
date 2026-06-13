//go:build !js

package ui

import (
	"testing"

	"github.com/AllenDang/cimgui-go/imgui"
)

// TestMonoFontGlyphCoverage verifies the embedded terminal font contains the
// box-drawing, braille, block, and symbol glyphs the default ProggyClean font
// lacks — the glyphs a rich TUI (e.g. Claude Code) renders. This is the fix for
// the "looks weird" rendering: the emulator was correct; the font was missing.
func TestMonoFontGlyphCoverage(t *testing.T) {
	imgui.CreateContext()
	defer imgui.DestroyContext()
	imgui.CurrentIO().SetIniFilename("") // don't write an imgui.ini in the package dir

	fonts := imgui.CurrentIO().Fonts()
	fonts.AddFontDefault()
	f := loadMonoFont(fonts, monoFontSize)
	if f == nil {
		t.Fatal("mono font failed to load (embedded asset missing?)")
	}

	// Covers both fonts: JetBrains Mono (box-drawing, blocks, shades, arrows,
	// symbols) and the Noto Symbols 2 merge fallback (braille spinner dots).
	for _, r := range []rune{
		'╭', '─', '│', '╰', '┤', // box drawing
		'░', '▒', '▓', '▌', // blocks/shades
		'⠋', '⠙', '⠹', '⣿', // braille spinner (fallback)
		'…', '✓', '●', '→', // symbols
	} {
		if !f.IsGlyphInFont(imgui.Wchar(r)) {
			t.Errorf("mono font missing glyph U+%04X %q", r, r)
		}
	}
}
