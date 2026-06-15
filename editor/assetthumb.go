package editor

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/plugin/ui"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Mesh thumbnails for the asset grid. Each mesh is rendered once into its own
// small offscreen target via a render3d.SceneView, then the view is disabled so
// the target keeps the last frame as a static image. They build one at a time
// off a single shared "thumb entity" parked far from the scene (thumbOrigin):
// the thumbnail camera frames only that spot, so the scene and the big-preview
// entity (at previewOrigin) both fall outside the view and the tile shows just
// the mesh.

const thumbPx = 96

// thumbOrigin is where the shared thumbnail entity sits — far from both the
// scene (origin) and the preview pane's entity (previewOrigin), so each camera
// frames only its own subject.
var thumbOrigin = m.Vec3{X: 100000, Y: 0, Z: 0}

type assetThumb struct {
	rt     *render3d.RenderTarget
	view   *render3d.SceneView
	texID  imgui.TextureID
	built  bool
	frames int
}

// thumbFor returns the cached thumbnail texture for a mesh key, or 0 while it is
// still rendering (the caller shows a placeholder until then).
func (st *state) thumbFor(c *app.Ctx, meshKey string) imgui.TextureID {
	if st.thumbs == nil {
		st.thumbs = make(map[string]*assetThumb)
	}
	t := st.thumbs[meshKey]
	if t == nil {
		t = &assetThumb{}
		st.thumbs[meshKey] = t
	}
	if t.built {
		return t.texID
	}
	// Build at most one thumbnail at a time — they share one entity/camera.
	if st.thumbBuilding != "" && st.thumbBuilding != meshKey {
		return 0
	}
	if !st.ensureThumbRig(c, t) {
		return 0
	}
	st.thumbBuilding = meshKey
	st.poseThumbEntity(c, meshKey)
	t.view.SetEnabled(true)
	t.frames++
	// Two enabled frames: the first lets transform propagation place the entity,
	// the second guarantees a rendered target before we freeze it.
	if t.frames >= 2 {
		t.texID = ui.RegisterTexture(c, t.rt.View())
		t.view.SetEnabled(false)
		t.built = true
		st.thumbBuilding = ""
	}
	return t.texID
}

// ensureThumbRig lazily builds the shared camera/entity and this thumb's target
// and view. Returns false until the GPU is ready.
func (st *state) ensureThumbRig(c *app.Ctx, t *assetThumb) bool {
	meshes := app.GetResource[render3d.MeshRegistry](c)
	if meshes == nil {
		return false
	}
	if st.thumbCam == nil {
		st.thumbCam = &render3d.Camera3D{FovYDeg: 35, Near: 0.05, Far: 1000, Up: m.Vec3{Y: 1}}
	}
	if st.thumbEntity == (ecs.Entity{}) || !c.World().Alive(st.thumbEntity) {
		st.thumbEntity = render3d.NewMesh(c,
			render3d.XForm().At(thumbOrigin.X, thumbOrigin.Y, thumbOrigin.Z), "", "")
	}
	if t.rt == nil {
		t.rt = render3d.NewRenderTarget(c, thumbPx, thumbPx)
		if t.rt == nil {
			return false
		}
		t.view = render3d.NewSceneView(c, t.rt, st.thumbCam)
		if t.view == nil {
			t.rt.Release(c)
			t.rt = nil
			return false
		}
		t.view.SetEnabled(false)
	}
	return true
}

// poseThumbEntity points the shared entity at meshKey, centers its bounds on
// thumbOrigin, and frames the thumbnail camera on it.
func (st *state) poseThumbEntity(c *app.Ctx, meshKey string) {
	w := c.World()
	e := st.thumbEntity
	mrMap := generic.NewMap[render3d.MeshRenderer](w)
	matMap := generic.NewMap[render3d.MaterialRef](w)
	tMap := generic.NewMap[render3d.Transform3D](w)
	mrMap.Get(e).Mesh = meshKey
	matMap.Get(e).Material = ""

	center := m.Vec3{}
	radius := float32(1)
	if lo, hi, ok := st.meshBounds(c, meshKey); ok {
		center = lo.Add(hi).Scale(0.5)
		radius = hi.Sub(lo).Length() * 0.5
		if radius <= 0 {
			radius = 1
		}
	}
	tMap.Get(e).Translation = thumbOrigin.Sub(center)
	st.thumbCam.SetAspect(1)
	st.thumbCam.Orbit(thumbOrigin, radius*2.8+0.4, 0.7, 0.5)
}

// invalidateThumb drops a mesh's cached thumbnail (and the image/material 2D
// caches) so it rebuilds — used when an asset is (re)imported or hot-reloaded.
func (st *state) invalidateThumb(c *app.Ctx, key string) {
	if t := st.thumbs[key]; t != nil {
		if t.texID != 0 {
			ui.UnregisterTexture(c, t.texID)
		}
		if t.view != nil {
			t.view.Release(c)
		}
		if t.rt != nil {
			t.rt.Release(c)
		}
		delete(st.thumbs, key)
	}
}
