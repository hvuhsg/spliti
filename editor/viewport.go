package editor

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/gizmo"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/plugin/ui"
	"github.com/mlange-42/arche/generic"
)

// drawViewport renders the Scene panel: the offscreen render target image,
// camera navigation, click picking, and the transform gizmo on the selection.
func drawViewport(c *app.Ctx, st *state) {
	imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, imgui.Vec2{})
	open := imgui.BeginV("Scene", nil,
		imgui.WindowFlagsNoScrollbar|imgui.WindowFlagsNoScrollWithMouse)
	imgui.PopStyleVar()
	if !open {
		imgui.End()
		return
	}

	avail := imgui.ContentRegionAvail()
	w, h := int(avail.X), int(avail.Y)
	if w < 16 || h < 16 {
		imgui.End()
		return
	}
	cam := app.GetResource[render3d.Camera3D](c)

	// Create / resize the offscreen target; the ImGui texture id stays stable
	// across resizes via ui.UpdateTexture.
	if st.rt == nil {
		st.rt = render3d.NewRenderTarget(c, w, h)
		if st.rt == nil {
			imgui.End()
			return
		}
		render3d.SetSceneTarget(c, st.rt)
		st.texID = ui.RegisterTexture(c, st.rt.View())
	} else if rw, rh := st.rt.Size(); rw != w || rh != h {
		st.rt.Resize(c, w, h)
		ui.UpdateTexture(c, st.texID, st.rt.View())
	}
	cam.SetAspect(float32(w) / float32(h))

	imageMin := imgui.CursorScreenPos()
	imgui.Image(*imgui.NewTextureRefTextureID(st.texID), avail)
	st.vpHovered = imgui.IsItemHovered()

	st.cam.handleInput(st.vpHovered)

	// Click picking — viewport-local pixels against the RT extents. Skipped
	// while the cursor is on a gizmo handle so grabs never re-pick.
	if st.vpHovered && imgui.IsMouseClickedBool(imgui.MouseButtonLeft) && !st.gz.Over() {
		mp := imgui.MousePos()
		origin, dir := cam.ScreenToRay(float64(mp.X-imageMin.X), float64(mp.Y-imageMin.Y), w, h)
		if hit, ok := render3d.Raycast(c, origin, dir); ok {
			st.selected, st.hasSelected = hit.Entity, true
		} else {
			st.hasSelected = false
		}
	}

	if st.hasSelected {
		drawSelectionGizmo(c, st, vec2(imageMin), vec2(avail))
	}

	imgui.End()
}

// drawSelectionGizmo runs the transform gizmo on the selected entity, writes
// the result into its (parent-relative) Transform3D, and schedules a source
// writeback when the drag ends.
func drawSelectionGizmo(c *app.Ctx, st *state, rectMin, rectSize m.Vec2) {
	world := c.World()
	if !world.Alive(st.selected) {
		st.hasSelected = false
		return
	}
	tMap := generic.NewMap[render3d.Transform3D](world)
	gtMap := generic.NewMap[render3d.GlobalTransform](world)
	if !tMap.Has(st.selected) || !gtMap.Has(st.selected) {
		return
	}
	cam := app.GetResource[render3d.Camera3D](c)
	model := gtMap.Get(st.selected).Matrix

	var snap float32
	if imgui.CurrentIO().KeyCtrl() {
		switch st.op {
		case gizmo.Rotate:
			snap = 15
		case gizmo.Scale:
			snap = 0.1
		default:
			snap = 0.5
		}
	}
	mouse := imgui.MousePos()
	f := gizmo.Frame{
		View:     cam.View(),
		Proj:     cam.Projection(),
		RectMin:  rectMin,
		RectSize: rectSize,
		MousePos: m.Vec2{X: mouse.X, Y: mouse.Y},
		// Poll the windowing layer: ImGui eats the release event, so an
		// ImGui-sourced "down" would leave the drag stuck.
		MouseDown: render3d.MouseButtonDown(c, inputs.MouseButtonLeft),
		Snap:      snap,
	}
	if st.gz.Manipulate(imgui.WindowDrawList(), f, st.op, gizmo.World, &model) {
		local := model
		// The gizmo manipulates the world matrix; re-localize under the
		// parent chain before storing the local transform.
		if pm := (generic.NewMap[render3d.Parent](world)); pm.Has(st.selected) {
			parent := pm.Get(st.selected).Entity
			if world.Alive(parent) && gtMap.Has(parent) {
				if inv, ok := gtMap.Get(parent).Matrix.Inverse(); ok {
					local = inv.Mul(model)
				}
			}
		}
		t := tMap.Get(st.selected)
		t.Translation, t.Rotation, t.Scale = decomposeTRS(local)
		// Keep the world matrix coherent within this frame (the renderer's
		// propagateTransforms recomputes it next frame anyway).
		gtMap.Get(st.selected).Matrix = model
	}
	if st.gz.Finished() {
		st.markTransformDirty(instanceName(c, st.selected))
	}
}
