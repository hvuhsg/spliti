package editor

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/macmenu"
)

// fileMenu holds handles to the native macOS "File" menu items so their
// enabled state can be synced with the editor each frame. On platforms without
// native menus the handles are inert and the same commands live in the in-app
// menu bar instead (see drawShell).
type fileMenu struct {
	built                    bool
	save, reload             *macmenu.Item
	exportNative, exportWasm *macmenu.Item
}

// buildNativeMenu installs the native "File" menu and wires its items to editor
// commands. It captures the (stable) Ctx so the menu callbacks, which fire on
// the main thread during the event pump and run on the next Dispatch, can reach
// the world. A no-op when native menus aren't available.
func (st *state) buildNativeMenu(c *app.Ctx) {
	if !macmenu.Available() {
		return
	}
	file := macmenu.Top("File")
	st.menu.save = file.Item("Save Scene", func() { st.saveNow(c, true) }).Key("s", macmenu.Cmd)
	st.menu.reload = file.Item("Reload Scene", func() { reloadScene(c, st) })
	file.Separator()

	exp := file.Submenu("Export")
	native, wasm := exportTargets[0], exportTargets[1]
	st.menu.exportNative = exp.Item(native.label, func() { st.startExport(native) })
	st.menu.exportWasm = exp.Item(wasm.label, func() { st.startExport(wasm) })

	st.menu.built = true
}

// refreshNativeMenu mirrors the editor state onto the native menu's enabled
// flags — the same gating the in-app buttons use (edit-mode only; export
// disabled while a build runs).
func (st *state) refreshNativeMenu() {
	if !st.menu.built {
		return
	}
	editing := st.mode == modeEdit
	st.menu.save.SetEnabled(editing)
	st.menu.reload.SetEnabled(editing)

	busy := st.export.running.Load() || st.rebuild.running.Load()
	st.menu.exportNative.SetEnabled(!busy)
	st.menu.exportWasm.SetEnabled(!busy)
}

// menuTick (schedule.First) runs the callbacks for any native-menu clicks since
// the last frame, then refreshes item state. A no-op when native menus are off.
func menuTick(c *app.Ctx) {
	macmenu.Dispatch()
	st := app.GetResource[state](c)
	st.refreshNativeMenu()
}
