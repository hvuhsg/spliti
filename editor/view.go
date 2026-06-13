package editor

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/macmenu"
)

// Font-scale bounds for the View menu's size controls. The scale multiplies the
// DPI font scale plugin/ui applies once at startup, so 1.0 is the auto-detected
// default and the user nudges from there.
const (
	fontScaleMin  = 0.7
	fontScaleMax  = 2.0
	fontScaleStep = 0.1
)

// viewState holds the user-tunable look of the editor — theme, UI font size, and
// whether the ground grid is drawn — plus handles to the native "View" menu
// items whose checked state mirrors it each frame. It is persisted in the
// session so a restart (or rebuild re-exec) keeps the chosen look (see
// session.go).
type viewState struct {
	themeIdx  int
	fontScale float32
	showGrid  bool

	// Native "View" menu handles; nil/inert off darwin (the same options live
	// in the in-app menu bar there, see drawViewMenuInApp).
	built     bool
	themeItem []*macmenu.Item
	gridItem  *macmenu.Item
}

// defaultView is the look applied to a fresh session: dark theme, default font
// size, grid on.
func defaultView() viewState {
	return viewState{themeIdx: 0, fontScale: 1, showGrid: true}
}

// theme returns the active theme, repairing an out-of-range index (e.g. from a
// session written by a build with more themes).
func (v *viewState) theme() editorTheme {
	if v.themeIdx < 0 || v.themeIdx >= len(editorThemes) {
		v.themeIdx = 0
	}
	return editorThemes[v.themeIdx]
}

// apply pushes the per-frame view settings that live on the ImGui style. The
// font-size multiplier (FontScaleMain) composes with plugin/ui's DPI font scale
// (FontScaleDpi), so the editor can resize text at runtime without touching the
// shared backend. Called at the top of editorUI.
func (v *viewState) apply() {
	imgui.CurrentStyle().SetFontScaleMain(clampFontScale(v.fontScale))
}

// zoomFont nudges the font size by d, clamped to the supported range.
func (v *viewState) zoomFont(d float32) { v.fontScale = clampFontScale(v.fontScale + d) }

// resetFont restores the auto-detected default size.
func (v *viewState) resetFont() { v.fontScale = 1 }

func clampFontScale(s float32) float32 {
	if s < fontScaleMin {
		return fontScaleMin
	}
	if s > fontScaleMax {
		return fontScaleMax
	}
	return s
}

// buildViewMenu installs the native macOS "View" menu (Theme, Font Size, grid
// toggle, layout reset). A no-op when native menus aren't available; the same
// options are drawn in-app on those platforms.
func (st *state) buildViewMenu(c *app.Ctx) {
	if !macmenu.Available() {
		return
	}
	view := macmenu.Top("View")

	theme := view.Submenu("Theme")
	st.view.themeItem = make([]*macmenu.Item, len(editorThemes))
	for i := range editorThemes {
		idx := i
		st.view.themeItem[i] = theme.Item(editorThemes[i].name, func() { st.view.themeIdx = idx })
	}
	view.Separator()

	size := view.Submenu("Font Size")
	size.Item("Increase", func() { st.view.zoomFont(fontScaleStep) }).Key("=", macmenu.Cmd)
	size.Item("Decrease", func() { st.view.zoomFont(-fontScaleStep) }).Key("-", macmenu.Cmd)
	size.Item("Reset", func() { st.view.resetFont() }).Key("0", macmenu.Cmd)
	view.Separator()

	st.view.gridItem = view.Item("Show Grid", func() { st.view.showGrid = !st.view.showGrid })
	view.Item("Reset Layout", func() { st.dockBuilt = false })

	st.view.built = true
}

// refreshViewMenu mirrors the editor's view state onto the native menu's
// checkmarks (active theme, grid on/off). Called each frame from menuTick.
func (st *state) refreshViewMenu() {
	if !st.view.built {
		return
	}
	for i, it := range st.view.themeItem {
		it.SetChecked(i == st.view.themeIdx)
	}
	st.view.gridItem.SetChecked(st.view.showGrid)
}

// drawViewMenuInApp draws the View menu inside the in-app menu bar. Used on
// platforms without a native menu bar (the darwin build puts these in the real
// menu bar instead, see buildViewMenu).
func drawViewMenuInApp(st *state) {
	if !imgui.BeginMenu("View") {
		return
	}
	if imgui.BeginMenu("Theme") {
		for i := range editorThemes {
			if imgui.MenuItemBoolV(editorThemes[i].name, "", i == st.view.themeIdx, true) {
				st.view.themeIdx = i
			}
		}
		imgui.EndMenu()
	}
	if imgui.BeginMenu("Font Size") {
		if imgui.MenuItemBool("Increase") {
			st.view.zoomFont(fontScaleStep)
		}
		if imgui.MenuItemBool("Decrease") {
			st.view.zoomFont(-fontScaleStep)
		}
		if imgui.MenuItemBool("Reset") {
			st.view.resetFont()
		}
		imgui.EndMenu()
	}
	imgui.Separator()
	if imgui.MenuItemBoolV("Show Grid", "", st.view.showGrid, true) {
		st.view.showGrid = !st.view.showGrid
	}
	if imgui.MenuItemBool("Reset Layout") {
		st.dockBuilt = false
	}
	imgui.EndMenu()
}
