package editor

import "github.com/AllenDang/cimgui-go/imgui"

// A modern dark theme for the editor. cimgui-go v1.5.0 exposes no setters on
// the persistent Style struct, so the look is applied per frame by pushing
// colors and style vars around the whole UI (pushEditorTheme) and popping the
// same counts afterwards (see editorUI). The palette is a cool, low-contrast
// slate with a single teal accent for interactive state.
var themeColors = []struct {
	id  imgui.Col
	col imgui.Vec4
}{
	{imgui.ColText, rgba(0xE4, 0xE7, 0xEC, 1)},
	{imgui.ColTextDisabled, rgba(0x6B, 0x72, 0x80, 1)},
	{imgui.ColWindowBg, rgba(0x16, 0x18, 0x1D, 1)},
	{imgui.ColChildBg, rgba(0x16, 0x18, 0x1D, 0)},
	{imgui.ColPopupBg, rgba(0x1C, 0x1F, 0x26, 0.98)},
	{imgui.ColBorder, rgba(0x2A, 0x2E, 0x37, 1)},
	{imgui.ColBorderShadow, rgba(0, 0, 0, 0)},
	{imgui.ColFrameBg, rgba(0x22, 0x26, 0x2E, 1)},
	{imgui.ColFrameBgHovered, rgba(0x2C, 0x31, 0x3B, 1)},
	{imgui.ColFrameBgActive, rgba(0x33, 0x39, 0x45, 1)},
	{imgui.ColTitleBg, rgba(0x12, 0x14, 0x18, 1)},
	{imgui.ColTitleBgActive, rgba(0x1A, 0x1D, 0x24, 1)},
	{imgui.ColTitleBgCollapsed, rgba(0x12, 0x14, 0x18, 1)},
	{imgui.ColMenuBarBg, rgba(0x1A, 0x1D, 0x24, 1)},
	{imgui.ColScrollbarBg, rgba(0x12, 0x14, 0x18, 0)},
	{imgui.ColScrollbarGrab, rgba(0x33, 0x39, 0x45, 1)},
	{imgui.ColScrollbarGrabHovered, rgba(0x3E, 0x45, 0x52, 1)},
	{imgui.ColScrollbarGrabActive, rgba(0x4A, 0x52, 0x60, 1)},
	{imgui.ColCheckMark, accent},
	{imgui.ColSliderGrab, accentDim},
	{imgui.ColSliderGrabActive, accent},
	{imgui.ColButton, rgba(0x2A, 0x2F, 0x39, 1)},
	{imgui.ColButtonHovered, rgba(0x35, 0x3C, 0x49, 1)},
	{imgui.ColButtonActive, accentDim},
	{imgui.ColHeader, rgba(0x2A, 0x2F, 0x39, 1)},
	{imgui.ColHeaderHovered, rgba(0x35, 0x3C, 0x49, 1)},
	{imgui.ColHeaderActive, accentDim},
	{imgui.ColSeparator, rgba(0x2A, 0x2E, 0x37, 1)},
	{imgui.ColSeparatorHovered, accentDim},
	{imgui.ColSeparatorActive, accent},
	{imgui.ColResizeGrip, rgba(0x2A, 0x2F, 0x39, 1)},
	{imgui.ColResizeGripHovered, accentDim},
	{imgui.ColResizeGripActive, accent},
	{imgui.ColTab, rgba(0x1A, 0x1D, 0x24, 1)},
	{imgui.ColTabHovered, rgba(0x35, 0x3C, 0x49, 1)},
	{imgui.ColTabSelected, rgba(0x26, 0x2C, 0x36, 1)},
	{imgui.ColTabSelectedOverline, accent},
	{imgui.ColTabDimmed, rgba(0x14, 0x16, 0x1B, 1)},
	{imgui.ColTabDimmedSelected, rgba(0x1E, 0x22, 0x29, 1)},
	{imgui.ColDockingPreview, accentDim},
	{imgui.ColDockingEmptyBg, rgba(0x0E, 0x0F, 0x12, 1)},
	{imgui.ColTextSelectedBg, accentDim},
	{imgui.ColNavCursor, accent},
}

// accent is the editor's single highlight color (teal); accentDim is its
// translucent variant used for pressed/active fills.
var (
	accent    = rgba(0x36, 0xC2, 0xB4, 1)
	accentDim = rgba(0x36, 0xC2, 0xB4, 0.55)
)

// rgba builds an ImGui color from 8-bit channels and a 0..1 alpha.
func rgba(r, g, b uint8, a float32) imgui.Vec4 {
	return imgui.Vec4{X: float32(r) / 255, Y: float32(g) / 255, Z: float32(b) / 255, W: a}
}

// pushEditorTheme applies the dark theme for the current frame and returns the
// (colors, vars) push counts so editorUI can pop exactly as many.
func pushEditorTheme() (colors, vars int32) {
	for _, e := range themeColors {
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
	return int32(len(themeColors)), vars
}
