package editor

import (
	"reflect"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Camera entities pose the game camera from the scene. Today the active camera
// is the global render3d.Camera3D resource; a Camera entity drives that resource
// each frame (render3d.applyCameraEntity), so the Game panel and a shipped game
// both see what the entity frames. The editor adds the icon/picking, an "Add
// Camera" affordance, and single-active selection on top of that.

// builtinCameraPrefab is the engine-level spawn function the editor writes for a
// new camera. It is not a //spliti:entity in the game, so newState injects it
// into the prefab map (the scene file already imports render3d for XForm, so the
// generated scene.Spawn line compiles).
const builtinCameraPrefab = "render3d.SpawnCamera"

// withBuiltinPrefabs returns the prefab map plus the engine-level builtins the
// editor can always spawn (the camera), without mutating the caller's map.
func withBuiltinPrefabs(prefabs map[string]PrefabFunc) map[string]PrefabFunc {
	out := make(map[string]PrefabFunc, len(prefabs)+1)
	for k, v := range prefabs {
		out[k] = v
	}
	if _, ok := out[builtinCameraPrefab]; !ok {
		out[builtinCameraPrefab] = render3d.SpawnCamera
	}
	return out
}

// cameraIconRadius is the pick radius of camera entities, which carry no mesh
// for the raycaster to hit.
const cameraIconRadius = float32(0.4)

// drawCameraIcons outlines every Camera entity in the scene's line pass: a small
// camera-body box at the eye plus a view volume pointing down its forward (-Z)
// axis — a converging cone for a perspective camera, a parallel box for an
// orthographic one. Brighter when the camera is the active one. The glyph
// doubles as a pick target (see pickCamera).
func drawCameraIcons(c *app.Ctx, st *state) {
	w := c.World()
	gtMap := generic.NewMap[render3d.GlobalTransform](w)
	app.Query1[render3d.Camera](c, func(e ecs.Entity, cm *render3d.Camera) {
		if !gtMap.Has(e) {
			return
		}
		drawCameraGlyph(c, gtMap.Get(e).Matrix, cameraColor(cm.Active), cm.Orthographic)
	})
}

// cameraAxes returns the camera's world-space right/up/forward axes and eye
// position, or ok=false when the forward axis is degenerate.
func cameraAxes(mat m.Mat4) (pos, right, up, fwd m.Vec3, ok bool) {
	fwd = render3d.Forward(mat)
	if fwd == (m.Vec3{}) {
		return pos, right, up, fwd, false
	}
	pos = m.Vec3{X: mat[12], Y: mat[13], Z: mat[14]}
	right = (m.Vec3{X: mat[0], Y: mat[1], Z: mat[2]}).Normalize()
	up = (m.Vec3{X: mat[4], Y: mat[5], Z: mat[6]}).Normalize()
	return pos, right, up, fwd, true
}

// drawCameraGlyph draws the body box and the view volume for one camera.
func drawCameraGlyph(c *app.Ctx, mat m.Mat4, col m.Vec4, ortho bool) {
	pos, right, up, fwd, ok := cameraAxes(mat)
	if !ok {
		return
	}
	rect := func(ctr m.Vec3, hw, hh float32) [4]m.Vec3 {
		return [4]m.Vec3{
			ctr.Add(right.Scale(hw)).Add(up.Scale(hh)),   // top-right
			ctr.Add(right.Scale(-hw)).Add(up.Scale(hh)),  // top-left
			ctr.Add(right.Scale(-hw)).Add(up.Scale(-hh)), // bottom-left
			ctr.Add(right.Scale(hw)).Add(up.Scale(-hh)),  // bottom-right
		}
	}
	loop := func(r [4]m.Vec3) {
		for i := 0; i < 4; i++ {
			render3d.Line(c, r[i], r[(i+1)%4], col)
		}
	}

	// Camera body: a small box sitting just behind the eye, so the view volume
	// emanates from its front face — reads as a little camera at a glance.
	const bw, bh, bd = cameraIconRadius * 0.6, cameraIconRadius * 0.45, cameraIconRadius * 0.9
	back := rect(pos.Sub(fwd.Scale(bd)), bw, bh)
	front := rect(pos, bw, bh)
	loop(back)
	loop(front)
	for i := 0; i < 4; i++ {
		render3d.Line(c, back[i], front[i], col)
	}
	// Viewfinder nub on top of the body, so roll is readable.
	nub := pos.Sub(fwd.Scale(bd * 0.5)).Add(up.Scale(bh))
	render3d.Line(c, nub, nub.Add(up.Scale(cameraIconRadius*0.5)), col)

	// View volume: a far rectangle, connected to the eye (cone) for perspective
	// or to a same-size near rectangle (parallel box) for orthographic.
	const d = cameraIconRadius * 2.4
	hw, hh := cameraIconRadius*1.4, cameraIconRadius
	far := rect(pos.Add(fwd.Scale(d)), hw, hh)
	loop(far)
	if ortho {
		near := rect(pos, hw, hh)
		loop(near)
		for i := 0; i < 4; i++ {
			render3d.Line(c, near[i], far[i], col)
		}
		return
	}
	for i := 0; i < 4; i++ {
		render3d.Line(c, pos, far[i], col)
	}
}

// cameraColor is bright cyan for the active camera, muted for the rest.
func cameraColor(active bool) m.Vec4 {
	if active {
		return m.Vec4{X: 0.35, Y: 0.85, Z: 1, W: 0.95}
	}
	return m.Vec4{X: 0.55, Y: 0.62, Z: 0.68, W: 0.7}
}

// pickCamera tests the pick ray against every camera's icon sphere so cameras
// are selectable without a mesh. Returns the nearest hit distance.
func pickCamera(c *app.Ctx, origin, dir m.Vec3) (ecs.Entity, float32, bool) {
	w := c.World()
	gtMap := generic.NewMap[render3d.GlobalTransform](w)
	d := dir.Normalize()
	var best ecs.Entity
	bestT := float32(0)
	found := false
	app.Query1[render3d.Camera](c, func(e ecs.Entity, _ *render3d.Camera) {
		if !gtMap.Has(e) {
			return
		}
		mat := gtMap.Get(e).Matrix
		p := m.Vec3{X: mat[12], Y: mat[13], Z: mat[14]}
		oc := p.Sub(origin)
		t := oc.Dot(d)
		if t <= 0 {
			return
		}
		if oc.Sub(d.Scale(t)).Length() > cameraIconRadius {
			return
		}
		if !found || t < bestT {
			best, bestT, found = e, t, true
		}
	})
	return best, bestT, found
}

// drawCameraFields renders the Camera component: a projection picker (which
// hides the size field that doesn't apply), the clip planes, and Active as a
// "Make active" button (single-active is editor-managed, so it is not a raw
// checkbox that could light up two cameras at once).
func drawCameraFields(fc fieldCtx, comp reflect.Value) {
	orthoField := comp.FieldByName("Orthographic")
	ortho := orthoField.Bool()
	// Projection picker: a combo over the Orthographic bool.
	if imgui.BeginCombo("Projection", projectionLabel(ortho)) {
		if imgui.SelectableBoolV(projectionLabel(false), !ortho, 0, imgui.Vec2{}) && ortho {
			orthoField.SetBool(false)
			fc.commitNow()
		}
		if imgui.SelectableBoolV(projectionLabel(true), ortho, 0, imgui.Vec2{}) && !ortho {
			orthoField.SetBool(true)
			fc.commitNow()
		}
		imgui.EndCombo()
	}
	for _, f := range fc.ti.Fields {
		switch f.Name {
		case "Active", "Orthographic":
			continue
		case "FovYDeg":
			if ortho {
				continue
			}
		case "OrthoSize":
			if !ortho {
				continue
			}
		}
		drawField(fc, f.Name, f.Kind, f.Value(comp))
	}
	if comp.FieldByName("Active").Bool() {
		imgui.TextColored(imgui.Vec4{X: 0.35, Y: 0.85, Z: 1, W: 1}, "active camera (drives the Game view)")
		return
	}
	if imgui.Button("Make active") {
		fc.st.makeActiveCamera(fc.c, fc.e)
	}
	imgui.SetItemTooltip("Drive the game camera from this entity (shown in the Game panel)")
}

// projectionLabel names a projection for the inspector combo.
func projectionLabel(ortho bool) string {
	if ortho {
		return "Orthographic"
	}
	return "Perspective"
}

// addCamera spawns a camera posed at the current editor view and makes it the
// active one — "create a camera from what I'm looking at". The new instance and
// the deactivation of any prior active camera are two undo steps.
func (st *state) addCamera(c *app.Ctx) {
	if st.mode != modeEdit {
		st.status("add camera is disabled during play")
		return
	}
	t := render3d.XForm()
	if cam := st.edCam; cam != nil {
		t = t.At(cam.Position.X, cam.Position.Y, cam.Position.Z).Facing(cam.Target.Sub(cam.Position))
	}
	e, ok := st.spawnPrefabT(c, builtinCameraPrefab, t)
	if ok {
		st.makeActiveCamera(c, e)
	}
}

// makeActiveCamera makes target the sole active camera: it sets target's Active
// true and every other camera's false, batched into one undo step. The drive
// system (render3d.applyCameraEntity) then renders the Game view from target.
func (st *state) makeActiveCamera(c *app.Ctx, target ecs.Entity) {
	ti := st.reg.Lookup("Camera")
	if ti == nil {
		return
	}
	w := c.World()
	var cmds []editorCmd
	app.Query1[render3d.Camera](c, func(e ecs.Entity, _ *render3d.Camera) {
		want := e == target
		cur, _ := ti.Value(w, e).(render3d.Camera)
		if cur.Active == want {
			return
		}
		after := cur
		after.Active = want
		cmds = append(cmds, st.newSetComponent(instanceName(c, e), e, ti, cur, after))
	})
	if len(cmds) > 0 {
		st.push(c, batch("set active camera", cmds))
	}
}
