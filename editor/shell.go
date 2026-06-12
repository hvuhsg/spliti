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

		if imgui.Button("Save") {
			st.saveNow(c, true)
		}
		imgui.SetItemTooltip("Write pending changes to the scene file now (Ctrl+S)")
		if imgui.Button("Reload") {
			reloadScene(c, st)
		}
		imgui.SetItemTooltip("Re-read the scene file from disk and sync the live world to it")
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
		var left, right, bottom, center imgui.ID
		imgui.InternalDockBuilderSplitNode(dockID, imgui.DirLeft, 0.2, &left, &center)
		imgui.InternalDockBuilderSplitNode(center, imgui.DirRight, 0.28, &right, &center)
		imgui.InternalDockBuilderSplitNode(left, imgui.DirDown, 0.35, &bottom, &left)
		imgui.InternalDockBuilderDockWindow("Hierarchy", left)
		imgui.InternalDockBuilderDockWindow("Assets", bottom)
		imgui.InternalDockBuilderDockWindow("Inspector", right)
		imgui.InternalDockBuilderDockWindow("Scene", center)
		imgui.InternalDockBuilderFinish(dockID)
	}
	// cimgui-go v1.5.0: DockSpaceV dereferences the WindowClass
	// unconditionally — nil segfaults, pass an empty one.
	imgui.DockSpaceV(dockID, imgui.Vec2{}, imgui.DockNodeFlagsNone, imgui.NewEmptyWindowClass())
	imgui.End()
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
