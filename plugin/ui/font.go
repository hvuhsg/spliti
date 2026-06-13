//go:build !js

package ui

import (
	_ "embed"
	"unsafe"

	"github.com/AllenDang/cimgui-go/imgui"
)

// monoTTF is JetBrains Mono (SIL OFL 1.1, see fonts/OFL.txt) — the Terminal
// panel's primary font. The editor's default font (ProggyClean) lacks the box-
// drawing, block, and symbol glyphs a rich TUI like Claude Code draws.
//
//go:embed fonts/JetBrainsMono-Regular.ttf
var monoTTF []byte

// fallbackTTF is Noto Sans Symbols 2 (SIL OFL 1.1, see
// fonts/NotoSansSymbols2-LICENSE.txt), merged over JetBrains Mono to cover
// symbol glyphs the latter lacks — notably braille (U+2800–28FF), which CLI
// spinners (incl. Claude Code's) use. JetBrains Mono still wins for Latin, box-
// drawing, and block/shade glyphs; this only fills the gaps.
//
//go:embed fonts/NotoSansSymbols2-Regular.ttf
var fallbackTTF []byte

// Fonts are kept as package vars: ImGui references the data (owned=false) for
// on-demand rasterization, so it must stay alive for the program's life.

// loadMonoFont adds the embedded monospace font to the atlas at sizePx, merges a
// broad-coverage fallback over it, and returns the primary font (nil if the
// asset is missing). Add it AFTER the default font so the default stays the rest
// of the UI's font; the Terminal panel pushes this one. With merge, the primary
// font wins for any glyph it has; the fallback fills only the gaps.
func loadMonoFont(fonts *imgui.FontAtlas, sizePx float32) *imgui.Font {
	if len(monoTTF) == 0 {
		return nil
	}
	cfg := imgui.NewFontConfig()
	cfg.SetFontDataOwnedByAtlas(false) // Go-owned embed; ImGui references, never frees
	font := fonts.AddFontFromMemoryTTFV(
		uintptr(unsafe.Pointer(&monoTTF[0])), int32(len(monoTTF)), sizePx, cfg, nil)

	if len(fallbackTTF) > 0 {
		fb := imgui.NewFontConfig()
		fb.SetFontDataOwnedByAtlas(false)
		fb.SetMergeMode(true) // merge into the font added just above
		fonts.AddFontFromMemoryTTFV(
			uintptr(unsafe.Pointer(&fallbackTTF[0])), int32(len(fallbackTTF)), sizePx, fb, nil)
	}
	return font
}

// monoFontSize is the unscaled base size; DPI scaling is applied on top via the
// global SetFontScaleDpi (see applyScale).
const monoFontSize = 14
