package editor

import "github.com/AllenDang/cimgui-go/imgui"

// The editor ships a few built-in themes (see editorThemes). cimgui-go v1.5.0
// exposes no setters on the persistent Style struct, so a theme is applied per
// frame by pushing colors and style vars around the whole UI (pushEditorTheme)
// and popping the same counts afterwards (see editorUI). The View menu switches
// the active theme (see view.go).

// colorEntry binds an ImGui color slot to a value for the per-frame theme push.
type colorEntry struct {
	id  imgui.Col
	col imgui.Vec4
}

// tones is the small set of semantic colors a theme is built from; buildColors
// expands them into the full ImGui color table. The pressed/active "accentDim"
// fills are derived from accent (same hue, lower alpha).
type tones struct {
	text, textDim                       imgui.Vec4
	windowBg, panelBg, border           imgui.Vec4
	frameBg, frameHover, frameActive    imgui.Vec4
	titleBg, titleBgActive              imgui.Vec4
	scrollHover, scrollActive           imgui.Vec4
	button, buttonHover                 imgui.Vec4
	tab, tabSelected, tabDim, tabDimSel imgui.Vec4
	dockEmpty                           imgui.Vec4
	accent                              imgui.Vec4
}

// editorTheme is a named, prebuilt color table.
type editorTheme struct {
	name   string
	colors []colorEntry
}

// withAlpha returns c with its alpha replaced.
func withAlpha(c imgui.Vec4, a float32) imgui.Vec4 { c.W = a; return c }

// buildColors expands a tone set into the full ImGui color table. Slots that
// share a tone in the original dark theme (e.g. button == header, scrollbar
// grab == frame-active) keep sharing it here so every theme stays internally
// consistent.
func buildColors(t tones) []colorEntry {
	accentDim := withAlpha(t.accent, 0.55)
	return []colorEntry{
		{imgui.ColText, t.text},
		{imgui.ColTextDisabled, t.textDim},
		{imgui.ColWindowBg, t.windowBg},
		{imgui.ColChildBg, withAlpha(t.windowBg, 0)},
		{imgui.ColPopupBg, t.panelBg},
		{imgui.ColBorder, t.border},
		{imgui.ColBorderShadow, rgba(0, 0, 0, 0)},
		{imgui.ColFrameBg, t.frameBg},
		{imgui.ColFrameBgHovered, t.frameHover},
		{imgui.ColFrameBgActive, t.frameActive},
		{imgui.ColTitleBg, t.titleBg},
		{imgui.ColTitleBgActive, t.titleBgActive},
		{imgui.ColTitleBgCollapsed, t.titleBg},
		{imgui.ColMenuBarBg, t.titleBgActive},
		{imgui.ColScrollbarBg, withAlpha(t.titleBg, 0)},
		{imgui.ColScrollbarGrab, t.frameActive},
		{imgui.ColScrollbarGrabHovered, t.scrollHover},
		{imgui.ColScrollbarGrabActive, t.scrollActive},
		{imgui.ColCheckMark, t.accent},
		{imgui.ColSliderGrab, accentDim},
		{imgui.ColSliderGrabActive, t.accent},
		{imgui.ColButton, t.button},
		{imgui.ColButtonHovered, t.buttonHover},
		{imgui.ColButtonActive, accentDim},
		{imgui.ColHeader, t.button},
		{imgui.ColHeaderHovered, t.buttonHover},
		{imgui.ColHeaderActive, accentDim},
		{imgui.ColSeparator, t.border},
		{imgui.ColSeparatorHovered, accentDim},
		{imgui.ColSeparatorActive, t.accent},
		{imgui.ColResizeGrip, t.button},
		{imgui.ColResizeGripHovered, accentDim},
		{imgui.ColResizeGripActive, t.accent},
		{imgui.ColTab, t.tab},
		{imgui.ColTabHovered, t.buttonHover},
		{imgui.ColTabSelected, t.tabSelected},
		{imgui.ColTabSelectedOverline, t.accent},
		{imgui.ColTabDimmed, t.tabDim},
		{imgui.ColTabDimmedSelected, t.tabDimSel},
		{imgui.ColDockingPreview, accentDim},
		{imgui.ColDockingEmptyBg, t.dockEmpty},
		{imgui.ColTextSelectedBg, accentDim},
		{imgui.ColNavCursor, t.accent},
	}
}

// darkTones is the editor's original cool slate-and-teal palette; buildColors
// reproduces the prior hand-tuned color table from it exactly.
var darkTones = tones{
	text:          rgba(0xE4, 0xE7, 0xEC, 1),
	textDim:       rgba(0x6B, 0x72, 0x80, 1),
	windowBg:      rgba(0x16, 0x18, 0x1D, 1),
	panelBg:       rgba(0x1C, 0x1F, 0x26, 0.98),
	border:        rgba(0x2A, 0x2E, 0x37, 1),
	frameBg:       rgba(0x22, 0x26, 0x2E, 1),
	frameHover:    rgba(0x2C, 0x31, 0x3B, 1),
	frameActive:   rgba(0x33, 0x39, 0x45, 1),
	titleBg:       rgba(0x12, 0x14, 0x18, 1),
	titleBgActive: rgba(0x1A, 0x1D, 0x24, 1),
	scrollHover:   rgba(0x3E, 0x45, 0x52, 1),
	scrollActive:  rgba(0x4A, 0x52, 0x60, 1),
	button:        rgba(0x2A, 0x2F, 0x39, 1),
	buttonHover:   rgba(0x35, 0x3C, 0x49, 1),
	tab:           rgba(0x1A, 0x1D, 0x24, 1),
	tabSelected:   rgba(0x26, 0x2C, 0x36, 1),
	tabDim:        rgba(0x14, 0x16, 0x1B, 1),
	tabDimSel:     rgba(0x1E, 0x22, 0x29, 1),
	dockEmpty:     rgba(0x0E, 0x0F, 0x12, 1),
	accent:        rgba(0x36, 0xC2, 0xB4, 1),
}

// lightTones is a clean, low-contrast light theme sharing the teal accent.
var lightTones = tones{
	text:          rgba(0x1C, 0x20, 0x26, 1),
	textDim:       rgba(0x8A, 0x92, 0x9E, 1),
	windowBg:      rgba(0xF3, 0xF4, 0xF6, 1),
	panelBg:       rgba(0xFF, 0xFF, 0xFF, 0.98),
	border:        rgba(0xD3, 0xD7, 0xDE, 1),
	frameBg:       rgba(0xFF, 0xFF, 0xFF, 1),
	frameHover:    rgba(0xEC, 0xEE, 0xF1, 1),
	frameActive:   rgba(0xE0, 0xE3, 0xE8, 1),
	titleBg:       rgba(0xE7, 0xE9, 0xED, 1),
	titleBgActive: rgba(0xDD, 0xE0, 0xE6, 1),
	scrollHover:   rgba(0xC2, 0xC7, 0xD0, 1),
	scrollActive:  rgba(0xA9, 0xAF, 0xBA, 1),
	button:        rgba(0xE4, 0xE7, 0xEB, 1),
	buttonHover:   rgba(0xD6, 0xDA, 0xE0, 1),
	tab:           rgba(0xE7, 0xE9, 0xED, 1),
	tabSelected:   rgba(0xFF, 0xFF, 0xFF, 1),
	tabDim:        rgba(0xDD, 0xDF, 0xE4, 1),
	tabDimSel:     rgba(0xEE, 0xF0, 0xF3, 1),
	dockEmpty:     rgba(0xDA, 0xDD, 0xE2, 1),
	accent:        rgba(0x12, 0x9E, 0x8E, 1),
}

// midnightTones is a deep blue-black dark theme with a violet accent.
var midnightTones = tones{
	text:          rgba(0xDC, 0xDF, 0xF0, 1),
	textDim:       rgba(0x6E, 0x74, 0x92, 1),
	windowBg:      rgba(0x10, 0x12, 0x1F, 1),
	panelBg:       rgba(0x16, 0x19, 0x2B, 0.98),
	border:        rgba(0x27, 0x2B, 0x44, 1),
	frameBg:       rgba(0x1C, 0x20, 0x36, 1),
	frameHover:    rgba(0x25, 0x2A, 0x45, 1),
	frameActive:   rgba(0x2E, 0x34, 0x55, 1),
	titleBg:       rgba(0x0C, 0x0E, 0x18, 1),
	titleBgActive: rgba(0x16, 0x19, 0x2B, 1),
	scrollHover:   rgba(0x39, 0x40, 0x66, 1),
	scrollActive:  rgba(0x45, 0x4D, 0x78, 1),
	button:        rgba(0x24, 0x29, 0x42, 1),
	buttonHover:   rgba(0x2F, 0x35, 0x55, 1),
	tab:           rgba(0x16, 0x19, 0x2B, 1),
	tabSelected:   rgba(0x22, 0x27, 0x40, 1),
	tabDim:        rgba(0x0E, 0x10, 0x1C, 1),
	tabDimSel:     rgba(0x1A, 0x1E, 0x32, 1),
	dockEmpty:     rgba(0x08, 0x09, 0x12, 1),
	accent:        rgba(0x8B, 0x7C, 0xF6, 1),
}

// editorThemes is the ordered list the View > Theme menu exposes; index 0 is
// the default applied to a fresh session.
var editorThemes = []editorTheme{
	{name: "Dark", colors: buildColors(darkTones)},
	{name: "Light", colors: buildColors(lightTones)},
	{name: "Midnight", colors: buildColors(midnightTones)},
}

// rgba builds an ImGui color from 8-bit channels and a 0..1 alpha.
func rgba(r, g, b uint8, a float32) imgui.Vec4 {
	return imgui.Vec4{X: float32(r) / 255, Y: float32(g) / 255, Z: float32(b) / 255, W: a}
}

// pushEditorTheme applies th for the current frame and returns the (colors,
// vars) push counts so editorUI can pop exactly as many.
func pushEditorTheme(th editorTheme) (colors, vars int32) {
	for _, e := range th.colors {
		imgui.PushStyleColorVec4(e.id, e.col)
	}
	pushVarF := func(id imgui.StyleVar, v float32) {
		imgui.PushStyleVarFloat(id, v)
		vars++
	}
	pushVarV := func(id imgui.StyleVar, v imgui.Vec2) {
		imgui.PushStyleVarVec2(id, v)
		vars++
	}
	pushVarF(imgui.StyleVarWindowRounding, 6)
	pushVarF(imgui.StyleVarChildRounding, 6)
	pushVarF(imgui.StyleVarPopupRounding, 6)
	pushVarF(imgui.StyleVarFrameRounding, 5)
	pushVarF(imgui.StyleVarGrabRounding, 5)
	pushVarF(imgui.StyleVarTabRounding, 5)
	pushVarF(imgui.StyleVarScrollbarRounding, 8)
	pushVarF(imgui.StyleVarFrameBorderSize, 0)
	pushVarF(imgui.StyleVarScrollbarSize, 12)
	pushVarF(imgui.StyleVarGrabMinSize, 11)
	pushVarF(imgui.StyleVarTabBarOverlineSize, 2)
	pushVarV(imgui.StyleVarWindowPadding, imgui.Vec2{X: 10, Y: 10})
	pushVarV(imgui.StyleVarFramePadding, imgui.Vec2{X: 8, Y: 5})
	pushVarV(imgui.StyleVarItemSpacing, imgui.Vec2{X: 8, Y: 7})
	pushVarV(imgui.StyleVarItemInnerSpacing, imgui.Vec2{X: 7, Y: 5})
	pushVarV(imgui.StyleVarWindowTitleAlign, imgui.Vec2{X: 0.02, Y: 0.5})
	return int32(len(th.colors)), vars
}
