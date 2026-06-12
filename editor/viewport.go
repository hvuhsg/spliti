package editor

import (
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/gizmo"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/plugin/ui"
	"github.com/mlange-42/arche/ecs"
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

	// Prefab drop from the Assets panel: spawn where the drop ray lands.
	if imgui.BeginDragDropTarget() {
		if payloadDelivered(imgui.AcceptDragDropPayload(prefabDragType)) && st.dragPrefab != "" {
			mp := imgui.MousePos()
			origin, dir := cam.ScreenToRay(float64(mp.X-imageMin.X), float64(mp.Y-imageMin.Y), w, h)
			st.spawnPrefab(c, st.dragPrefab, dropPoint(c, origin, dir))
			st.dragPrefab = ""
		}
		imgui.EndDragDropTarget()
	}

	st.cam.handleInput(st.vpHovered)

	// Click picking — viewport-local pixels against the RT extents. Skipped
	// while the cursor is on a gizmo handle so grabs never re-pick.
	// Ctrl/cmd-click toggles set membership for multi-selection.
	if st.vpHovered && imgui.IsMouseClickedBool(imgui.MouseButtonLeft) && !st.gz.Over() {
		io := imgui.CurrentIO()
		multi := io.KeyCtrl() || io.KeySuper()
		mp := imgui.MousePos()
		origin, dir := cam.ScreenToRay(float64(mp.X-imageMin.X), float64(mp.Y-imageMin.Y), w, h)
		picked, ok := pickEntity(c, origin, dir)
		switch {
		case ok && multi:
			st.toggleSelect(picked)
		case ok:
			st.selectOne(picked)
		case !multi:
			st.clearSelection()
		}
	}

	if len(st.sel) > 0 {
		drawSelectionGizmo(c, st, vec2(imageMin), vec2(avail))
	}

	imgui.End()
}

// drawSelectionGizmo runs the transform gizmo anchored on the primary
// selection. The primary follows the gizmo directly; every other selected
// entity follows through the drag's world-space delta matrix. One batched
// undo command covers the whole drag.
func drawSelectionGizmo(c *app.Ctx, st *state, rectMin, rectSize m.Vec2) {
	world := c.World()
	prim, ok := st.primary()
	if !ok {
		return
	}
	tMap := generic.NewMap[render3d.Transform3D](world)
	gtMap := generic.NewMap[render3d.GlobalTransform](world)
	if !tMap.Has(prim) || !gtMap.Has(prim) {
		return
	}
	cam := app.GetResource[render3d.Camera3D](c)
	model := gtMap.Get(prim).Matrix
	prevModel := model

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
	changed := st.gz.Manipulate(imgui.WindowDrawList(), f, st.op, gizmo.World, &model)
	if st.gz.Started() {
		// Capture every selected entity's pre-drag local transform for undo.
		clear(st.dragStart)
		for _, e := range st.sel {
			if tMap.Has(e) {
				st.dragStart[e] = *tMap.Get(e)
			}
		}
	}
	if changed {
		t := tMap.Get(prim)
		t.Translation, t.Rotation, t.Scale = decomposeTRS(localize(c, prim, model))
		// Keep the world matrix coherent within this frame (the renderer's
		// propagateTransforms recomputes it next frame anyway).
		gtMap.Get(prim).Matrix = model
		// The rest of the selection follows by this frame's world-space delta.
		if inv, ok := prevModel.Inverse(); ok {
			delta := model.Mul(inv)
			for _, e := range st.sel {
				if e == prim || !tMap.Has(e) || !gtMap.Has(e) || followsSelected(c, st, e) {
					continue
				}
				ng := delta.Mul(gtMap.Get(e).Matrix)
				et := tMap.Get(e)
				et.Translation, et.Rotation, et.Scale = decomposeTRS(localize(c, e, ng))
				gtMap.Get(e).Matrix = ng
			}
		}
	}
	if st.gz.Finished() {
		var cmds []editorCmd
		for _, e := range st.sel {
			before, ok := st.dragStart[e]
			if !ok || !tMap.Has(e) {
				continue
			}
			after := *tMap.Get(e)
			if before == after {
				continue
			}
			cmds = append(cmds, &cmdTransform{
				instance: instanceName(c, e),
				entity:   e,
				before:   before,
				after:    after,
			})
		}
		clear(st.dragStart)
		if len(cmds) > 0 {
			st.push(c, batch("transform", cmds))
		}
	}
}

// followsSelected reports whether e already inherits the drag through a
// selected ancestor — applying the delta again would double-move it.
func followsSelected(c *app.Ctx, st *state, e ecs.Entity) bool {
	for _, other := range st.sel {
		if other != e && isDescendant(c, e, other) {
			return true
		}
	}
	return false
}

// pickEntity resolves a pick ray to the nearest entity: mesh raycast and
// light-icon spheres compete by distance, so meshless lights are selectable.
func pickEntity(c *app.Ctx, origin, dir m.Vec3) (ecs.Entity, bool) {
	hit, hitOK := render3d.Raycast(c, origin, dir)
	light, lightT, lightOK := pickLight(c, origin, dir)
	switch {
	case hitOK && lightOK:
		if lightT < hit.Dist {
			return light, true
		}
		return hit.Entity, true
	case hitOK:
		return hit.Entity, true
	case lightOK:
		return light, true
	}
	return ecs.Entity{}, false
}

// localize re-expresses a world matrix in the entity's parent space (parents
// do not move during a drag, so this is valid for both drag start and end).
func localize(c *app.Ctx, e ecs.Entity, model m.Mat4) m.Mat4 {
	world := c.World()
	pm := generic.NewMap[render3d.Parent](world)
	if !pm.Has(e) {
		return model
	}
	parent := pm.Get(e).Entity
	gtMap := generic.NewMap[render3d.GlobalTransform](world)
	if !world.Alive(parent) || !gtMap.Has(parent) {
		return model
	}
	if inv, ok := gtMap.Get(parent).Matrix.Inverse(); ok {
		return inv.Mul(model)
	}
	return model
}

// spawnPrefab pushes a spawn command placing a new instance of the prefab at
// the given world point.
func (st *state) spawnPrefab(c *app.Ctx, prefab string, at m.Vec3) {
	base := strings.TrimPrefix(prefab, "entities.")
	base = strings.TrimPrefix(base, "Spawn")
	if base == "" {
		base = "instance"
	}
	base = strings.ToLower(base[:1]) + base[1:]
	name := base
	if _, exists := entityByInstance(c, name); exists {
		name = st.freeInstanceName(c, base)
	} else if sc := st.scene(); sc != nil && sc.Spawn(name) != nil {
		name = st.freeInstanceName(c, base)
	}
	cmd := &cmdSpawn{instance: name, prefab: prefab, t: render3d.XForm().At(at.X, at.Y, at.Z)}
	st.push(c, cmd)
	if e, ok := entityByInstance(c, name); ok {
		st.selectOne(e)
	}
}

// spawnPointFallback is where double-click spawns land.
var spawnPointFallback = m.Vec3{}

// dropPoint resolves a drop ray to a world position: the first mesh hit, else
// the ground plane (y = 0), else a point a few units down the ray.
func dropPoint(c *app.Ctx, origin, dir m.Vec3) m.Vec3 {
	if hit, ok := render3d.Raycast(c, origin, dir); ok {
		return hit.Point
	}
	d := dir.Normalize()
	if d.Y < -1e-4 && origin.Y > 0 {
		t := -origin.Y / d.Y
		return origin.Add(d.Scale(t))
	}
	return origin.Add(d.Scale(6))
}
