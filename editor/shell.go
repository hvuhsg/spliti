package editor

import (
	"fmt"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/gizmo"
)

// drawShell lays the fullscreen dock host with the menu/tool bar and builds
// the default layout (Hierarchy left, Inspector right, Scene center) once.
func drawShell(c *app.Ctx, st *state) {
	vp := imgui.MainViewport()
	imgui.SetNextWindowPos(vp.Pos())
	imgui.SetNextWindowSize(vp.Size())
	imgui.PushStyleVarFloat(imgui.StyleVarWindowRounding, 0)
	imgui.PushStyleVarFloat(imgui.StyleVarWindowBorderSize, 0)
	imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, imgui.Vec2{})
	imgui.BeginV("##editor-dockhost", nil,
		imgui.WindowFlagsNoDecoration|imgui.WindowFlagsNoMove|
			imgui.WindowFlagsNoBringToFrontOnFocus|imgui.WindowFlagsNoNavFocus|
			imgui.WindowFlagsNoDocking|imgui.WindowFlagsMenuBar)
	imgui.PopStyleVarV(3)

	if imgui.BeginMenuBar() {
		imgui.TextUnformatted("spliti")
		imgui.Separator()
		sceneLabel := fmt.Sprintf("scene: %s", st.cfg.Scene)
		if st.unsaved() {
			sceneLabel += " *"
		}
		imgui.TextUnformatted(sceneLabel)
		imgui.SetItemTooltip("* = unsaved changes (auto-saved after a moment; Ctrl+S to save now)")
		imgui.Separator()
		gizmoModeButtons(st)
		imgui.Separator()
		playControls(c, st)
		imgui.Separator()

		undoName, canUndo := st.undo.CanUndo()
		if !canUndo {
			imgui.BeginDisabled()
		}
		if imgui.Button("Undo") {
			st.undoLast(c)
		}
		if canUndo {
			imgui.SetItemTooltip("Undo " + undoName + " (Ctrl+Z)")
		} else {
			imgui.EndDisabled()
		}
		redoName, canRedo := st.undo.CanRedo()
		if !canRedo {
			imgui.BeginDisabled()
		}
		if imgui.Button("Redo") {
			st.redo(c)
		}
		if canRedo {
			imgui.SetItemTooltip("Redo " + redoName + " (Ctrl+Shift+Z)")
		} else {
			imgui.EndDisabled()
		}
		imgui.Separator()

		editing := st.mode == modeEdit
		if !editing {
			imgui.BeginDisabled()
		}
		if imgui.Button("Save") {
			st.saveNow(c, true)
		}
		imgui.SetItemTooltip("Write pending changes to the scene file now (Ctrl+S)")
		if imgui.Button("Reload") {
			reloadScene(c, st)
		}
		imgui.SetItemTooltip("Re-read the scene file from disk and sync the live world to it")
		if !editing {
			imgui.EndDisabled()
		}
		if st.rebuildNeeded || st.rebuild.running.Load() {
			imgui.Separator()
			rebuildBanner(c, st)
		}
		if st.srcErr != nil {
			imgui.SameLine()
			imgui.TextColored(imgui.Vec4{X: 1, Y: 0.35, Z: 0.3, W: 1},
				fmt.Sprintf("scene read-only: %v", st.srcErr))
		} else if st.statusMsg != "" && time.Since(st.statusAt) < 4*time.Second {
			imgui.SameLine()
			imgui.TextColored(imgui.Vec4{X: 0.6, Y: 0.9, Z: 0.6, W: 1}, st.statusMsg)
		}
		imgui.EndMenuBar()
	}

	dockID := imgui.IDStr("spliti-editor-dock")
	if !st.dockBuilt {
		st.dockBuilt = true
		imgui.InternalDockBuilderRemoveNode(dockID)
		imgui.InternalDockBuilderAddNodeV(dockID, imgui.DockNodeFlags(imgui.DockNodeFlagsNone))
		imgui.InternalDockBuilderSetNodeSize(dockID, imgui.ContentRegionAvail())
		var left, right, bottom, lower, center imgui.ID
		imgui.InternalDockBuilderSplitNode(dockID, imgui.DirLeft, 0.2, &left, &center)
		imgui.InternalDockBuilderSplitNode(center, imgui.DirRight, 0.28, &right, &center)
		imgui.InternalDockBuilderSplitNode(center, imgui.DirDown, 0.25, &lower, &center)
		imgui.InternalDockBuilderSplitNode(left, imgui.DirDown, 0.35, &bottom, &left)
		imgui.InternalDockBuilderDockWindow("Hierarchy", left)
		imgui.InternalDockBuilderDockWindow("Assets", bottom)
		imgui.InternalDockBuilderDockWindow("Systems", bottom)
		imgui.InternalDockBuilderDockWindow("Layers", bottom)
		imgui.InternalDockBuilderDockWindow("Input", bottom)
		imgui.InternalDockBuilderDockWindow("Inspector", right)
		imgui.InternalDockBuilderDockWindow("Console", lower)
		imgui.InternalDockBuilderDockWindow("Scene", center)
		imgui.InternalDockBuilderFinish(dockID)
	}
	// cimgui-go v1.5.0: DockSpaceV dereferences the WindowClass
	// unconditionally — nil segfaults, pass an empty one.
	imgui.DockSpaceV(dockID, imgui.Vec2{}, imgui.DockNodeFlagsNone, imgui.NewEmptyWindowClass())
	imgui.End()
}

// playControls is the Play/Pause/Step/Stop cluster. Play snapshots the world
// and starts the game systems; Stop restores the snapshot.
func playControls(c *app.Ctx, st *state) {
	switch st.mode {
	case modeEdit:
		if imgui.Button("Play") {
			st.startPlay(c)
		}
		imgui.SetItemTooltip("Run the game systems (Ctrl+P); the scene is snapshotted and restored on Stop")
	case modePlaying, modePaused:
		label := "Pause"
		if st.mode == modePaused {
			label = "Resume"
		}
		if imgui.Button(label) {
			st.togglePause()
		}
		if st.mode != modePaused {
			imgui.BeginDisabled()
		}
		if imgui.Button("Step") {
			st.requestStep()
		}
		imgui.SetItemTooltip("Advance the game one frame")
		if st.mode != modePaused {
			imgui.EndDisabled()
		}
		if imgui.Button("Stop") {
			st.stopPlay(c)
		}
		imgui.SetItemTooltip("Stop and restore the pre-play scene (Ctrl+P)")
		imgui.SameLine()
		if st.mode == modePaused {
			imgui.TextColored(imgui.Vec4{X: 1, Y: 0.8, Z: 0.4, W: 1}, "PAUSED")
		} else {
			imgui.TextColored(imgui.Vec4{X: 0.45, Y: 0.9, Z: 0.5, W: 1}, "PLAYING")
		}
	}
}

// rebuildBanner appears when game code changed on disk (or a rebuild is in
// flight): structural changes need a recompile and re-exec to reach the editor.
func rebuildBanner(c *app.Ctx, st *state) {
	if st.rebuild.running.Load() {
		imgui.TextColored(imgui.Vec4{X: 0.75, Y: 0.8, Z: 1, W: 1}, "rebuilding...")
		return
	}
	imgui.TextColored(imgui.Vec4{X: 1, Y: 0.8, Z: 0.4, W: 1}, "game code changed")
	imgui.SameLine()
	if imgui.Button("Rebuild & Restart") {
		st.startRebuild(c)
	}
	imgui.SetItemTooltip("Regenerate and recompile the editor target, then restart with the session restored")
}

func gizmoModeButtons(st *state) {
	mode := func(label string, op int) {
		sel := int(st.op) == op
		if sel {
			imgui.PushStyleColorVec4(imgui.ColButton, *imgui.StyleColorVec4(imgui.ColButtonActive))
		}
		if imgui.Button(label) {
			st.op = gizmo.Op(op)
		}
		if sel {
			imgui.PopStyleColor()
		}
	}
	mode("move (W)", 0)
	mode("rotate (E)", 1)
	mode("scale (R)", 2)
}
