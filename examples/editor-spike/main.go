// Command editor-spike is the de-risk spike for the spliti editor: it proves
// the three load-bearing editor mechanisms end to end —
//
//  1. the 3D scene rendering into an offscreen render3d.RenderTarget shown
//     inside a docked ImGui "Scene" panel (render-to-texture + DockBuilder),
//  2. click picking with viewport-local coordinates (Camera3D.ScreenToRay
//     against the target's extents + render3d.Raycast),
//  3. the hand-rolled editor/gizmo manipulating an entity's Transform3D
//     (translate/rotate/scale with W/E/R, Ctrl to snap). The editor ships its
//     own gizmo instead of the bundled ImGuizmo binding.
//
// Requires a GPU window, cgo, and a C/C++ toolchain:
//
//	CGO_ENABLED=1 go run ./examples/editor-spike
package main

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/gizmo"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/plugin/screenshot"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/ui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

func init() { runtime.LockOSThread() }

// editorState is the spike's scratch resource: the offscreen viewport target,
// its ImGui texture id, the dock layout flag, the selection, the gizmo mode,
// and the orbit-camera parameters.
type editorState struct {
	rt    *render3d.RenderTarget
	texID imgui.TextureID

	dockBuilt bool

	selected    ecs.Entity
	hasSelected bool

	op gizmo.Op
	gz gizmo.Gizmo

	camYaw, camPitch, camDist float32
	camPivot                  m.Vec3
}

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{TargetFrameRate: 0},
		render3d.Plugin{
			Width: 1600, Height: 900,
			Title:      "spliti — editor spike",
			ClearColor: render3d.Color{R: 0.02, G: 0.03, B: 0.05, A: 1},
			Ambient:    m.Vec3{X: 0.04, Y: 0.05, Z: 0.06},
			Samples:    4,
			VSync:      true,
		},
		ui.Plugin{},
	)
	app.InsertResource(a, &editorState{
		op:       gizmo.Translate,
		camYaw:   0.6,
		camPitch: 0.5,
		camDist:  9,
	})

	a.AddSystems(schedule.Startup, setup)
	a.AddSystems(schedule.Update, editorUI)
	a.AddSystems(schedule.Update, quitOnEscape)
	render3d.AddPreRender(a, applyCamera)

	// Smoke-test hooks: bound the run so CI / scripts can verify the editor
	// pipeline (offscreen pass + dockspace + gizmo) starts and renders, and
	// optionally dump a mid-run screenshot for visual inspection.
	if n, _ := strconv.Atoi(os.Getenv("SPLITI_SPIKE_FRAMES")); n > 0 {
		a.SetMaxFrames(n)
	}
	if shot := os.Getenv("SPLITI_SPIKE_SHOT"); shot != "" {
		a.AddSystems(schedule.Update, func(c *app.Ctx) {
			// Select the cube partway in so the screenshot proves the gizmo
			// renders, then capture.
			if c.App().Frame() == 60 {
				ed := app.GetResource[editorState](c)
				app.Query2[render3d.MeshRenderer, render3d.Transform3D](c,
					func(e ecs.Entity, mr *render3d.MeshRenderer, _ *render3d.Transform3D) {
						if mr.Mesh == "cube" {
							ed.selected, ed.hasSelected = e, true
						}
					})
			}
			switch c.App().Frame() {
			case 90, 120, 200:
				screenshot.Save(c, fmt.Sprintf("%s.%d.png", shot, c.App().Frame()))
			}
		})
	}

	a.Run()
}

func setup(c *app.Ctx) {
	// Docking is opt-in; the ui plugin's defaults don't enable it. The ImGui
	// context exists once ui.Plugin.Build has run, so Startup is early enough.
	io := imgui.CurrentIO()
	io.SetConfigFlags(imgui.ConfigFlagsNavEnableKeyboard | imgui.ConfigFlagsDockingEnable)

	meshes := app.GetResource[render3d.MeshRegistry](c)
	materials := app.GetResource[render3d.MaterialRegistry](c)
	must(meshes.Load("cube", render3d.Cube(1.2)))
	must(meshes.Load("sphere", render3d.UVSphere(0.75, 48, 32)))
	must(meshes.Load("plane", render3d.Plane(20, 20, 1, 1)))
	must(materials.Load("metal", render3d.Material{
		BaseColor: render3d.Color{R: 1.0, G: 0.78, B: 0.34, A: 1},
		Metallic:  1.0, Roughness: 0.25,
	}))
	must(materials.Load("plastic", render3d.Material{
		BaseColor: render3d.Color{R: 0.85, G: 0.15, B: 0.18, A: 1},
		Metallic:  0.0, Roughness: 0.4,
	}))
	must(materials.Load("ground", render3d.Material{
		BaseColor: render3d.Color{R: 0.5, G: 0.5, B: 0.55, A: 1},
		Metallic:  0.0, Roughness: 0.9,
	}))

	cmd := c.Commands()
	render3d.SpawnMesh(cmd, render3d.NewTransform3D(m.Vec3{}), "plane", "ground")
	render3d.SpawnMesh(cmd, render3d.NewTransform3D(m.Vec3{X: -1.4, Y: 0.6}), "cube", "metal")
	render3d.SpawnMesh(cmd, render3d.NewTransform3D(m.Vec3{X: 1.4, Y: 0.75}), "sphere", "plastic")
	render3d.SpawnDirectionalLight(cmd, render3d.DirectionalLight{
		Direction: m.Vec3{X: -0.4, Y: -1, Z: -0.3},
		Color:     m.Vec3{X: 1, Y: 0.98, Z: 0.92},
		Intensity: 3.0,
	})
}

// applyCamera writes the orbit parameters into the Camera3D each frame, before
// transform propagation / frame-uniform upload.
func applyCamera(c *app.Ctx) {
	ed := app.GetResource[editorState](c)
	cam := app.GetResource[render3d.Camera3D](c)
	if cam == nil {
		return
	}
	cam.Orbit(ed.camPivot, ed.camDist, ed.camYaw, ed.camPitch)
}

// editorUI draws the dockspace shell, the Objects panel, and the Scene viewport
// panel (offscreen target + picking + gizmo).
func editorUI(c *app.Ctx) {
	ed := app.GetResource[editorState](c)

	// W/E/R switch the gizmo operation (unless typing in a widget).
	if !imgui.CurrentIO().WantCaptureKeyboard() {
		switch {
		case imgui.IsKeyPressedBool(imgui.KeyW):
			ed.op = gizmo.Translate
		case imgui.IsKeyPressedBool(imgui.KeyE):
			ed.op = gizmo.Rotate
		case imgui.IsKeyPressedBool(imgui.KeyR):
			ed.op = gizmo.Scale
		}
	}

	dockID := drawDockHost(ed)
	drawObjectsPanel(c, ed)
	drawScenePanel(c, ed)
	_ = dockID
}

// drawDockHost lays a fullscreen, undecorated host window over the main
// viewport and opens a dockspace in it. On the first frame it builds the
// default layout: Objects docked left, Scene filling the center.
func drawDockHost(ed *editorState) imgui.ID {
	vp := imgui.MainViewport()
	imgui.SetNextWindowPos(vp.Pos())
	imgui.SetNextWindowSize(vp.Size())
	imgui.PushStyleVarFloat(imgui.StyleVarWindowRounding, 0)
	imgui.PushStyleVarFloat(imgui.StyleVarWindowBorderSize, 0)
	imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, imgui.Vec2{})
	imgui.BeginV("##dockhost", nil,
		imgui.WindowFlagsNoDecoration|imgui.WindowFlagsNoMove|
			imgui.WindowFlagsNoBringToFrontOnFocus|imgui.WindowFlagsNoNavFocus|
			imgui.WindowFlagsNoDocking)
	imgui.PopStyleVarV(3)

	dockID := imgui.IDStr("editor-spike-dock")
	if !ed.dockBuilt {
		ed.dockBuilt = true
		imgui.InternalDockBuilderRemoveNode(dockID)
		imgui.InternalDockBuilderAddNodeV(dockID, imgui.DockNodeFlags(imgui.DockNodeFlagsNone))
		imgui.InternalDockBuilderSetNodeSize(dockID, imgui.ContentRegionAvail())
		var left, center imgui.ID
		imgui.InternalDockBuilderSplitNode(dockID, imgui.DirLeft, 0.22, &left, &center)
		imgui.InternalDockBuilderDockWindow("Objects", left)
		imgui.InternalDockBuilderDockWindow("Scene", center)
		imgui.InternalDockBuilderFinish(dockID)
	}
	// Note: DockSpaceV with a nil *WindowClass segfaults in cimgui-go v1.5.0
	// (the wrapper dereferences it unconditionally) — pass an empty one.
	imgui.DockSpaceV(dockID, imgui.Vec2{}, imgui.DockNodeFlagsNone, imgui.NewEmptyWindowClass())
	imgui.End()
	return dockID
}

// drawObjectsPanel lists every renderable entity and lets the user select one,
// mirroring what the real hierarchy panel will do.
func drawObjectsPanel(c *app.Ctx, ed *editorState) {
	imgui.Begin("Objects")
	imgui.TextUnformatted(fmt.Sprintf("gizmo: %s  (W/E/R)", opName(ed.op)))
	imgui.Separator()
	app.Query2[render3d.MeshRenderer, render3d.Transform3D](c,
		func(e ecs.Entity, mr *render3d.MeshRenderer, _ *render3d.Transform3D) {
			label := fmt.Sprintf("%s##%v", mr.Mesh, e)
			sel := ed.hasSelected && ed.selected == e
			if imgui.SelectableBoolV(label, sel, 0, imgui.Vec2{}) {
				ed.selected, ed.hasSelected = e, true
			}
		})
	imgui.End()
}

// drawScenePanel renders the offscreen viewport image, sizes the RenderTarget
// to the panel, and runs picking, the orbit camera, and the gizmo against the
// image rect.
func drawScenePanel(c *app.Ctx, ed *editorState) {
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

	// Create / resize the offscreen target to the panel size; keep the ImGui
	// texture id stable across resizes via ui.UpdateTexture.
	if ed.rt == nil {
		ed.rt = render3d.NewRenderTarget(c, w, h)
		if ed.rt == nil {
			imgui.End()
			return
		}
		render3d.SetSceneTarget(c, ed.rt)
		ed.texID = ui.RegisterTexture(c, ed.rt.View())
	} else if rw, rh := ed.rt.Size(); rw != w || rh != h {
		ed.rt.Resize(c, w, h)
		ui.UpdateTexture(c, ed.texID, ed.rt.View())
	}
	cam.SetAspect(float32(w) / float32(h))

	imageMin := imgui.CursorScreenPos()
	imgui.Image(*imgui.NewTextureRefTextureID(ed.texID), avail)
	hovered := imgui.IsItemHovered()

	// Orbit camera: right-drag rotates, wheel zooms — only while the cursor is
	// over the viewport image.
	if hovered {
		io := imgui.CurrentIO()
		if imgui.IsMouseDraggingV(imgui.MouseButtonRight, 0) {
			d := io.MouseDelta()
			ed.camYaw -= d.X * 0.008
			ed.camPitch += d.Y * 0.008
			ed.camPitch = clamp(ed.camPitch, -1.5, 1.5)
		}
		if wheel := io.MouseWheel(); wheel != 0 {
			ed.camDist *= 1 - wheel*0.1
			ed.camDist = clamp(ed.camDist, 1, 60)
		}
	}

	// Click picking: viewport-local pixel -> ray (RT extents) -> nearest mesh.
	// Skipped while the gizmo is hot so grabbing a handle never re-picks.
	if hovered && imgui.IsMouseClickedBool(imgui.MouseButtonLeft) && !ed.gz.Over() {
		mp := imgui.MousePos()
		lx := float64(mp.X - imageMin.X)
		ly := float64(mp.Y - imageMin.Y)
		origin, dir := cam.ScreenToRay(lx, ly, w, h)
		if hit, ok := render3d.Raycast(c, origin, dir); ok {
			ed.selected, ed.hasSelected = hit.Entity, true
		} else {
			ed.hasSelected = false
		}
	}

	// Transform gizmo on the selection, drawn into this window over the image.
	if ed.hasSelected {
		drawGizmoFor(c, ed, imageMin, avail)
	}

	imgui.End()
}

// drawGizmoFor runs the transform gizmo for the selected entity and writes the
// manipulated matrix back into its Transform3D (spike entities have no Parent,
// so the global matrix is the local one).
func drawGizmoFor(c *app.Ctx, ed *editorState, imageMin, size imgui.Vec2) {
	world := c.World()
	if !world.Alive(ed.selected) {
		ed.hasSelected = false
		return
	}
	tMap := generic.NewMap[render3d.Transform3D](world)
	gtMap := generic.NewMap[render3d.GlobalTransform](world)
	if !tMap.Has(ed.selected) || !gtMap.Has(ed.selected) {
		return
	}

	cam := app.GetResource[render3d.Camera3D](c)
	model := gtMap.Get(ed.selected).Matrix

	// Ctrl snaps: half-units, 15 degrees, scale tenths.
	var snap float32
	if imgui.CurrentIO().KeyCtrl() {
		switch ed.op {
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
		RectMin:  m.Vec2{X: imageMin.X, Y: imageMin.Y},
		RectSize: m.Vec2{X: size.X, Y: size.Y},
		MousePos: m.Vec2{X: mouse.X, Y: mouse.Y},
		// Poll the windowing layer: ImGui eats the release event, so an
		// ImGui-sourced "down" would leave the drag stuck.
		MouseDown: render3d.MouseButtonDown(c, inputs.MouseButtonLeft),
		Snap:      snap,
	}
	if ed.gz.Manipulate(imgui.WindowDrawList(), f, ed.op, gizmo.World, &model) {
		t := tMap.Get(ed.selected)
		t.Translation, t.Rotation, t.Scale = decomposeTRS(model)
		// Write the global matrix too so the gizmo and render stay coherent
		// within this frame (propagateTransforms recomputes it anyway).
		gtMap.Get(ed.selected).Matrix = model
	}
}

// decomposeTRS splits a column-major TRS matrix into translation, rotation, and
// scale. Assumes no shear/negative scale (true for gizmo-produced matrices).
func decomposeTRS(mat m.Mat4) (m.Vec3, m.Quat, m.Vec3) {
	t := m.Vec3{X: mat[12], Y: mat[13], Z: mat[14]}
	cx := m.Vec3{X: mat[0], Y: mat[1], Z: mat[2]}
	cy := m.Vec3{X: mat[4], Y: mat[5], Z: mat[6]}
	cz := m.Vec3{X: mat[8], Y: mat[9], Z: mat[10]}
	s := m.Vec3{X: cx.Length(), Y: cy.Length(), Z: cz.Length()}
	if s.X == 0 || s.Y == 0 || s.Z == 0 {
		return t, m.IdentityQuat(), m.Vec3{X: 1, Y: 1, Z: 1}
	}
	r := quatFromAxes(cx.Scale(1/s.X), cy.Scale(1/s.Y), cz.Scale(1/s.Z))
	return t, r, s
}

// quatFromAxes builds a quaternion from an orthonormal rotation basis (the
// classic Shepperd / trace method).
func quatFromAxes(x, y, z m.Vec3) m.Quat {
	// Rotation matrix entries (column-major basis vectors).
	m00, m10, m20 := x.X, x.Y, x.Z
	m01, m11, m21 := y.X, y.Y, y.Z
	m02, m12, m22 := z.X, z.Y, z.Z

	trace := m00 + m11 + m22
	var q m.Quat
	switch {
	case trace > 0:
		s := float32(math.Sqrt(float64(trace+1))) * 2
		q = m.Quat{W: s / 4, X: (m21 - m12) / s, Y: (m02 - m20) / s, Z: (m10 - m01) / s}
	case m00 > m11 && m00 > m22:
		s := float32(math.Sqrt(float64(1+m00-m11-m22))) * 2
		q = m.Quat{W: (m21 - m12) / s, X: s / 4, Y: (m01 + m10) / s, Z: (m02 + m20) / s}
	case m11 > m22:
		s := float32(math.Sqrt(float64(1+m11-m00-m22))) * 2
		q = m.Quat{W: (m02 - m20) / s, X: (m01 + m10) / s, Y: s / 4, Z: (m12 + m21) / s}
	default:
		s := float32(math.Sqrt(float64(1+m22-m00-m11))) * 2
		q = m.Quat{W: (m10 - m01) / s, X: (m02 + m20) / s, Y: (m12 + m21) / s, Z: s / 4}
	}
	return q.Normalize()
}

func opName(op gizmo.Op) string {
	switch op {
	case gizmo.Rotate:
		return "rotate"
	case gizmo.Scale:
		return "scale"
	default:
		return "translate"
	}
}

func quitOnEscape(c *app.Ctx) {
	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Key == inputs.KeyEscape && ev.Action == inputs.Press {
			c.App().Stop()
		}
	}
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
