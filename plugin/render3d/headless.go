package render3d

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/schedule"
)

// finishBuildHeadless is the GPU-less counterpart of finishBuild: it installs the
// same resources (registries, camera, gizmo/overlay handles, asset loader) and
// the world-mutating systems, but creates no wgpu instance, surface, adapter, or
// device and opens no window. It exists so a 3D game's scene setup (LoadAssets +
// spawn funcs that reference the registries) runs on a machine with no GPU or
// display — the agent verify loop (`spliti check`) on headless CI.
//
// The registries operate in CPU-only mode when their GPU's device is nil: meshes
// keep their geometry and bounding sphere, materials keep their CPU record, and
// nothing is uploaded. The render/present/input systems are deliberately not
// installed (they need the device and a swapchain); only the systems that mutate
// ECS state the world dump captures run — animation sampling, transform
// propagation, and the camera-entity → Camera3D drive.
func finishBuildHeadless(p Plugin, a *app.App) *GPU {
	width, height, _ := p.sizeDefaults()

	ambient := p.Ambient
	if ambient == (m.Vec3{}) {
		ambient = m.Vec3{X: 0.03, Y: 0.03, Z: 0.03}
	}
	g := &GPU{
		headless:  true,
		headlessW: width,
		headlessH: height,
		ambient:   ambient,
		keysDown:  make(map[inputs.Key]bool),
	}
	g.buttonsDown = make(map[inputs.MouseButton]bool)

	cam := defaultCamera(p, float32(width)/float32(height))

	meshes := newMeshRegistry(g)
	materials := newMaterialRegistry(g)
	g.meshes = meshes
	g.materials = materials

	app.InsertResource(a, g)
	app.InsertResource(a, cam)
	app.InsertResource(a, meshes)
	app.InsertResource(a, materials)
	g.gizmo = newGizmoBuffer(g)
	g.overlay = newOverlay2D(g)
	app.InsertResource(a, g.gizmo)
	app.InsertResource(a, g.overlay)

	loader := newAssetLoader(meshes, materials, false)
	app.InsertResource(a, loader)

	a.AddOnExit(func() {
		loader.close()
		releaseGPU(g)
	})

	installHeadlessSystems(a)
	installLoaderSystem(a)
	return g
}

// installHeadlessSystems installs only the render3d systems that mutate ECS state
// (so the headless world dump matches a real run): animation sampling, transform
// propagation, and applyCameraEntity. The GPU-bound systems (input poll, joint
// packing, frame-uniform upload, render, gizmo/overlay draw, present) are skipped
// — there is no device or swapchain to feed.
func installHeadlessSystems(a *app.App) {
	a.AddSystems(schedule.PostUpdate,
		app.System(advanceAnimations).Label(animLabel).Before(transformLabel))
	a.AddSystems(schedule.PostUpdate,
		app.System(newPropagateTransforms()).Label(transformLabel))
	a.AddSystems(schedule.PostUpdate,
		app.System(applyCameraEntity).Label(cameraLabel).After(transformLabel))
}
