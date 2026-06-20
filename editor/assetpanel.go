package editor

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/audio"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/plugin/ui"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// The asset preview pane renders the selected model (or material on a sphere)
// into an offscreen target via a dedicated render3d.SceneView, and previews
// images and audio inline. The preview entity lives far from the scene
// (previewOrigin) so the tight preview camera frames only it, leaving the game
// world untouched and unsaved (it carries no scene.Name).

// previewOrigin is where the offscreen preview entity sits — far above any
// gameplay geometry, so the main viewport and Game view frustum-cull it.
var previewOrigin = m.Vec3{X: 0, Y: 100000, Z: 0}

// previewSphereRef is the editor-internal mesh a material preview is shown on.
const previewSphereRef = reservedAssetPrefix + "preview_sphere"

// waveform is a decoded audio clip reduced to per-bin peak amplitudes.
type waveform struct {
	peaks []float32
	dur   time.Duration
	err   error
}

// drawAssetPreview shows a preview of the panel's selected asset.
func (st *state) drawAssetPreview(c *app.Ctx) {
	key := st.selAsset
	if key == "" {
		imgui.TextDisabled("select an asset to preview")
		st.disablePreview()
		return
	}
	if mats := app.GetResource[render3d.MaterialRegistry](c); mats != nil && hasKey(mats.Keys(), key) {
		st.preview3D(c, previewSphereRef, key)
		st.drawMaterialEditor(c, key)
		return
	}
	if meshes := app.GetResource[render3d.MeshRegistry](c); meshes != nil && hasKey(meshes.Keys(), key) {
		st.preview3D(c, key, "")
		return
	}
	switch classify(key) {
	case classImage:
		st.disablePreview()
		if id := st.imageTexFor(c, key, key); id != 0 {
			imgui.Image(*imgui.NewTextureRefTextureID(id), st.previewSize(196))
		} else {
			imgui.TextDisabled("could not decode image")
		}
	case classAudio:
		st.disablePreview()
		if imgui.Button("Play") {
			st.playAudio(c, key)
		}
		st.drawWaveform(key)
	default:
		imgui.TextDisabled("no preview")
		st.disablePreview()
	}
}

// preview3D renders meshKey (with optional matKey) into the preview pane on a
// slow turntable. Falls back to a text line until the GPU/rig is ready.
func (st *state) preview3D(c *app.Ctx, meshKey, matKey string) {
	side := int(st.previewSize(220).X)
	if side < 32 {
		side = 32
	}
	if !st.ensurePreviewRig(c, side, side) {
		imgui.TextDisabled("preview unavailable")
		return
	}
	w := c.World()
	e := st.prevEntity
	mrMap := generic.NewMap[render3d.MeshRenderer](w)
	matMap := generic.NewMap[render3d.MaterialRef](w)
	tMap := generic.NewMap[render3d.Transform3D](w)
	mrMap.Get(e).Mesh = meshKey
	matMap.Get(e).Material = matKey

	center := m.Vec3{}
	radius := float32(1)
	if lo, hi, ok := st.meshBounds(c, meshKey); ok {
		center = lo.Add(hi).Scale(0.5)
		radius = hi.Sub(lo).Length() * 0.5
		if radius <= 0 {
			radius = 1
		}
	}
	// Place the entity so its bounds center lands on previewOrigin.
	tMap.Get(e).Translation = previewOrigin.Sub(center)

	st.prevYaw += 0.01
	st.prevCam.Orbit(previewOrigin, radius*2.8+0.4, st.prevYaw, 0.45)
	st.prevView.SetEnabled(true)
	imgui.Image(*imgui.NewTextureRefTextureID(st.prevTexID), st.previewSize(220))
}

// ensurePreviewRig lazily builds the offscreen target, view, camera, sphere
// mesh, and the reusable preview entity. Returns false until the GPU is ready.
func (st *state) ensurePreviewRig(c *app.Ctx, w, h int) bool {
	meshes := app.GetResource[render3d.MeshRegistry](c)
	if meshes == nil {
		return false
	}
	if !hasKey(meshes.Keys(), previewSphereRef) {
		_ = meshes.Load(previewSphereRef, render3d.UVSphere(0.7, 32, 24))
	}
	if st.prevCam == nil {
		st.prevCam = &render3d.Camera3D{FovYDeg: 35, Near: 0.05, Far: 1000, Up: m.Vec3{Y: 1}}
	}
	if st.prevRT == nil {
		st.prevRT = render3d.NewRenderTarget(c, w, h)
		if st.prevRT == nil {
			return false
		}
		st.prevView = render3d.NewSceneView(c, st.prevRT, st.prevCam)
		if st.prevView == nil {
			st.prevRT.Release(c)
			st.prevRT = nil
			return false
		}
		st.prevTexID = ui.RegisterTexture(c, st.prevRT.View())
	} else if rw, rh := st.prevRT.Size(); rw != w || rh != h {
		st.prevRT.Resize(c, w, h)
		ui.UpdateTexture(c, st.prevTexID, st.prevRT.View())
	}
	if st.prevEntity == (ecs.Entity{}) || !c.World().Alive(st.prevEntity) {
		st.prevEntity = render3d.NewMesh(c,
			render3d.XForm().At(previewOrigin.X, previewOrigin.Y, previewOrigin.Z), "", "")
	}
	st.prevCam.SetAspect(float32(w) / float32(h))
	return true
}

// disablePreview stops the offscreen pass when no 3D asset is shown.
func (st *state) disablePreview() {
	if st.prevView != nil {
		st.prevView.SetEnabled(false)
	}
}

// previewSize returns a square preview size clamped to the available width.
func (st *state) previewSize(max float32) imgui.Vec2 {
	w := imgui.ContentRegionAvail().X
	if w > max {
		w = max
	}
	if w < 32 {
		w = 32
	}
	return imgui.Vec2{X: w, Y: w}
}

// drawMaterialEditor edits the selected material's properties. Changes apply
// live every frame (so the sphere preview updates); the source line and an undo
// entry are written once the gesture ends.
func (st *state) drawMaterialEditor(c *app.Ctx, key string) {
	if st.assets == nil {
		imgui.TextDisabled("material defined outside //spliti:assets")
		return
	}
	e := st.assets.Entry(key)
	if e == nil {
		imgui.TextDisabled("material not in LoadAssets")
		return
	}
	src, ok := e.Material()
	if !ok {
		imgui.TextDisabled("non-literal material - not editable")
		return
	}
	cur := src
	if staged, editing := st.matStage[key]; editing {
		cur = staged
	}

	changed, done := false, false
	col := [4]float32{cur.R, cur.G, cur.B, cur.A}
	if imgui.ColorEdit4V("Base color", &col, colorFlags) {
		cur.R, cur.G, cur.B, cur.A = col[0], col[1], col[2], col[3]
		changed = true
	}
	done = done || imgui.IsItemDeactivatedAfterEdit()
	if imgui.DragFloatV("Metallic", &cur.Metallic, 0.01, 0, 1, "%.2f", 0) {
		changed = true
	}
	done = done || imgui.IsItemDeactivatedAfterEdit()
	if imgui.DragFloatV("Roughness", &cur.Roughness, 0.01, 0, 1, "%.2f", 0) {
		changed = true
	}
	done = done || imgui.IsItemDeactivatedAfterEdit()
	if imgui.Checkbox("Double sided", &cur.DoubleSided) {
		changed = true
		done = true // checkbox has no drag gesture
	}

	if changed {
		st.matStage[key] = cur
		if r := app.GetResource[render3d.MaterialRegistry](c); r != nil {
			_ = r.Load(key, matFromSpec(cur))
		}
	}
	if done {
		delete(st.matStage, key)
		if cur != src {
			st.push(c, &cmdSetMaterial{key: key, before: src, after: cur})
		}
	}
}

// materialColor is a material's base color (for the grid swatch tile), or a
// neutral grey when it isn't an editable literal.
func (st *state) materialColor(key string) imgui.Vec4 {
	col := imgui.Vec4{X: 0.7, Y: 0.7, Z: 0.72, W: 1}
	if st.assets != nil {
		if e := st.assets.Entry(key); e != nil {
			if s, ok := e.Material(); ok {
				col = imgui.Vec4{X: s.R, Y: s.G, Z: s.B, W: s.A}
			}
		}
	}
	return col
}

// drawWaveform draws the audio file's amplitude envelope.
func (st *state) drawWaveform(file string) {
	wf := st.waveformFor(file)
	if wf.err != nil {
		imgui.TextDisabled("waveform: " + wf.err.Error())
		return
	}
	width := imgui.ContentRegionAvail().X
	if width < 32 {
		width = 32
	}
	const height = float32(40)
	p0 := imgui.CursorScreenPos()
	imgui.Dummy(imgui.Vec2{X: width, Y: height})
	n := len(wf.peaks)
	if n == 0 {
		return
	}
	dl := imgui.WindowDrawList()
	col := imgui.ColorConvertFloat4ToU32(imgui.Vec4{X: 0.4, Y: 0.7, Z: 1, W: 0.9})
	mid := p0.Y + height/2
	for i, pk := range wf.peaks {
		x := p0.X + width*float32(i)/float32(n)
		h := pk * (height / 2)
		dl.AddLineV(imgui.Vec2{X: x, Y: mid - h}, imgui.Vec2{X: x, Y: mid + h}, col, 1)
	}
}

// waveformFor decodes (and caches) the waveform of an audio file.
func (st *state) waveformFor(file string) waveform {
	if w, ok := st.waveCache[file]; ok {
		return w
	}
	var w waveform
	data, err := os.ReadFile(filepath.Join(st.cfg.ProjectRoot, file))
	if err != nil {
		w.err = err
	} else {
		w.peaks, w.dur, w.err = audio.DecodeWaveform(data, 200)
	}
	st.waveCache[file] = w
	return w
}

// imageTexFor decodes (and caches) an image file as an ImGui texture; 0 on
// failure.
func (st *state) imageTexFor(c *app.Ctx, key, file string) imgui.TextureID {
	if id, ok := st.assetTex[key]; ok {
		return id
	}
	var id imgui.TextureID
	if f, err := os.Open(filepath.Join(st.cfg.ProjectRoot, file)); err == nil {
		if img, _, derr := image.Decode(f); derr == nil {
			id = ui.NewImageTexture(c, img)
		}
		f.Close()
	}
	st.assetTex[key] = id
	return id
}

// playAudio plays an audio file through the game's mixer when present.
func (st *state) playAudio(c *app.Ctx, file string) {
	au := app.GetResource[audio.Audio](c)
	if au == nil {
		st.status("audio plugin not loaded - cannot play")
		return
	}
	data, err := os.ReadFile(filepath.Join(st.cfg.ProjectRoot, file))
	if err != nil {
		st.status(err.Error())
		return
	}
	ref := "__preview:" + file
	if err := au.Registry().Load(ref, data); err != nil {
		st.status(err.Error())
		return
	}
	au.Play(ref)
}

// hasKey reports whether keys contains k.
func hasKey(keys []string, k string) bool {
	for _, x := range keys {
		if x == k {
			return true
		}
	}
	return false
}
