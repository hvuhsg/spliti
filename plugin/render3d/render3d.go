// Package render3d is a 3D GPU render backend for spliti, built along the lines
// of a real game engine: a perspective camera, a depth buffer, indexed triangle
// meshes with vertex normals, PBR metallic-roughness shading with directional
// and point lights, and GPU instancing.
//
// It is a sibling of the 2D plugin/webgpu backend and reuses the same
// render-as-a-plugin seam: Build installs a *GPU resource plus a few systems
// (input poll in First; transform propagation, frame-uniform upload, and a
// render/present split in PostUpdate). spliti keeps driving app.Run() and the
// schedule — this plugin never takes over the loop.
//
// An entity renders when it has Transform3D + MeshRenderer (MaterialRef
// optional, defaulting to a neutral material). MeshRenderer.Mesh names a mesh in
// the MeshRegistry; MaterialRef.Material names a material in the
// MaterialRegistry. Lights are entities with DirectionalLight or PointLight.
//
// Build targets match plugin/webgpu: native desktop (CGO_ENABLED=1, a C
// toolchain) via the bundled wgpu-native library and go-gl/glfw for the window
// and input (render3d_native.go), and the browser under GOOS=js GOARCH=wasm via
// the page's native WebGPU and DOM input (render3d_js.go). The platform-neutral
// GPU pipeline, rendering, and resources are shared in the untagged files.
// Input is reported as backend-agnostic plugin/inputs events.
//
// Models load from glTF/GLB via LoadGLTF / LoadGLTFFS (textured PBR materials —
// base-color, metallic-roughness, and normal maps — and a node hierarchy
// rebuilt with SpawnModel). Off-screen entities are skipped each frame by
// bounding-sphere frustum culling. The architecture still leaves clean seams for
// further additions (shadow mapping, HDR post-processing, mipmaps).
package render3d

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// Plugin installs the 3D GPU window, renderer, and input poll.
type Plugin struct {
	Width, Height int    // initial window size in pixels (defaults 1280x720)
	Title         string // window title (defaults "spliti")

	// Camera configuration. FovYDeg is the vertical field of view in degrees
	// (default 60). Near/Far are the clip distances (defaults 0.1 / 1000). The
	// initial Position/Target/Up frame the scene; games typically drive the
	// Camera3D resource each frame instead.
	FovYDeg, Near, Far   float32
	Position, Target, Up m.Vec3

	// ClearColor fills the framebuffer each frame. Zero value is opaque black.
	ClearColor Color

	// Ambient is a constant ambient light term (a stand-in for image-based
	// lighting). Zero value defaults to a dim neutral ambient so unlit faces are
	// not pure black.
	Ambient m.Vec3

	// VSync caps presentation to the display refresh (FIFO). When false the
	// renderer presents as fast as the surface allows (Immediate), falling back
	// to FIFO if the platform lacks it.
	VSync bool

	// Samples requests multisample anti-aliasing (MSAA). WebGPU portably supports
	// 1 (off) and 4; any value >1 is treated as 4. Zero/1 leaves MSAA off.
	Samples int
}

// Color is an RGBA color in linear-ish [0,1] components, used for the clear
// color and material base colors.
type Color struct{ R, G, B, A float32 }

func (c Color) orOpaque() Color {
	if c.A == 0 && c.R == 0 && c.G == 0 && c.B == 0 {
		return Color{0, 0, 0, 1}
	}
	return c
}

// GPU is the shared rendering resource. Systems read it via app.GetResource[GPU].
// All fields are owned by the plugin; treat as opaque.
type GPU struct {
	// plat holds platform-specific handles (the GLFW window on native, the
	// canvas + DOM callbacks on js). Its layout is defined per build tag.
	plat platform

	instance *wgpu.Instance
	adapter  *wgpu.Adapter
	surface  *wgpu.Surface
	device   *wgpu.Device
	queue    *wgpu.Queue
	config   *wgpu.SurfaceConfiguration

	pipeline *wgpu.RenderPipeline
	// transparentPipeline shares the layout and shader but blends with depth-write
	// off, for the back-to-front translucent pass after opaque geometry.
	transparentPipeline *wgpu.RenderPipeline
	// format is the swapchain/pipeline color format; depthFormat is the
	// depth-stencil attachment format. Both kept so the pipeline can be rebuilt
	// (the MSAA fallback) without re-deriving them.
	format      wgpu.TextureFormat
	depthFormat wgpu.TextureFormat

	// samples is the MSAA sample count baked into the pipeline AND the depth
	// target (they must match). When >1 the render pass draws into msaaTex and
	// resolves to the swapchain.
	samples  uint32
	msaaTex  *wgpu.Texture
	msaaView *wgpu.TextureView

	depthTex  *wgpu.Texture
	depthView *wgpu.TextureView

	// Per-instance model matrices, uploaded once per frame and indexed by draw.
	instanceBuf *wgpu.Buffer
	instanceCap int // capacity in instances (mat4s)

	// Frame uniform (camera + directional light + ambient + point count) and the
	// point-light storage buffer.
	frameBuf       *wgpu.Buffer
	pointBuf       *wgpu.Buffer
	pointCap       int // capacity in point lights
	frameBindGroup *wgpu.BindGroup
	frameBGL       *wgpu.BindGroupLayout
	materialBGL    *wgpu.BindGroupLayout

	meshes    *MeshRegistry // back-references so teardown frees GPU resources
	materials *MaterialRegistry
	gizmo     *GizmoBuffer
	overlay   *Overlay2D

	clearColor wgpu.Color
	ambient    m.Vec3

	// Resize handshake: the framebuffer-size callback (main thread) sets these;
	// the input system applies them before the next frame.
	pendingW, pendingH int
	resized            bool

	// Input buffers filled by the platform callbacks (GLFW on native, DOM on js)
	// and drained by the input system in the same (main) goroutine — no locking
	// needed.
	keyEvents   []inputs.KeyEvent
	mouseButton []inputs.MouseButtonEvent
	mouseMove   []inputs.MouseMoveEvent

	// keysDown tracks currently-held keys, updated from key events as they drain,
	// so games can poll held state (KeyDown) portably instead of the native-only
	// GLFW window.
	keysDown map[inputs.Key]bool

	cursorX, cursorY float64

	// Reusable per-frame scratch (collected renderables, packed model matrices,
	// mesh+material batches, packed point lights) to avoid steady-state allocation.
	items     []renderItem
	tItems    []renderItem // transparent items, sorted back-to-front each frame
	scratch   []instanceData
	batches   []meshBatch
	collected []collectedPoint
	lights    []pointLightGPU
	frameUBO  [frameUBOFloats]float32

	// viewFrustum is the current frame's six clipping planes, rebuilt each frame
	// in drawMeshes and used to cull entities whose world bounds fall outside it.
	viewFrustum frustum

	// Open frame state, shared between the render and present systems (both run on
	// the main goroutine in PostUpdate). frameActive is false when the surface
	// texture couldn't be acquired and the frame must be skipped.
	curTex      *wgpu.Texture
	curView     *wgpu.TextureView
	curEncoder  *wgpu.CommandEncoder
	curPass     *wgpu.RenderPassEncoder
	frameActive bool
}

// sizeDefaults returns the plugin's window size and title with zero values
// replaced by defaults. The platform Build uses it before creating the
// window/canvas.
func (p Plugin) sizeDefaults() (width, height int, title string) {
	width, height = p.Width, p.Height
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 720
	}
	title = p.Title
	if title == "" {
		title = "spliti"
	}
	return width, height, title
}

// finishBuild performs the platform-neutral setup shared by the native and js
// Build paths: adapter/device/queue, surface configuration at the given
// framebuffer pixel size, the pipeline, MSAA and depth targets, the camera, mesh
// and material registries, the gizmo and overlay buffers, resource insertion,
// and system installation. The caller has already created the instance and
// surface (from a GLFW window or an HTML canvas) and must fill g.plat and wire
// the platform input callbacks after this returns.
func finishBuild(p Plugin, a *app.App, instance *wgpu.Instance, surface *wgpu.Surface, fbw, fbh int) *GPU {
	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{
		CompatibleSurface: surface,
	})
	if err != nil {
		panic("render3d: request adapter: " + err.Error())
	}
	device, err := adapter.RequestDevice(nil)
	if err != nil {
		panic("render3d: request device: " + err.Error())
	}
	queue := device.GetQueue()

	caps := surface.GetCapabilities(adapter)
	present := wgpu.PresentModeFifo
	if !p.VSync && containsPresentMode(caps.PresentModes, wgpu.PresentModeImmediate) {
		present = wgpu.PresentModeImmediate
	}
	config := &wgpu.SurfaceConfiguration{
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      pickSurfaceFormat(caps.Formats),
		Width:       uint32(fbw),
		Height:      uint32(fbh),
		PresentMode: present,
		AlphaMode:   caps.AlphaModes[0],
	}
	surface.Configure(adapter, device, config)

	cc := p.ClearColor.orOpaque()
	ambient := p.Ambient
	if ambient == (m.Vec3{}) {
		ambient = m.Vec3{X: 0.03, Y: 0.03, Z: 0.03}
	}
	g := &GPU{
		instance:    instance,
		adapter:     adapter,
		surface:     surface,
		device:      device,
		queue:       queue,
		config:      config,
		format:      config.Format,
		depthFormat: wgpu.TextureFormatDepth24Plus,
		samples:     normalizeSamples(p.Samples),
		ambient:     ambient,
		clearColor: wgpu.Color{
			R: float64(cc.R), G: float64(cc.G), B: float64(cc.B), A: float64(cc.A),
		},
	}

	buildPipeline(g)
	ensureMSAATarget(g)
	ensureDepthTarget(g)

	cam := defaultCamera(p, float32(fbw)/float32(fbh))

	g.keysDown = make(map[inputs.Key]bool)

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

	a.AddOnExit(func() { releaseGPU(g) })

	installSystems(a)
	return g
}

// pickSurfaceFormat prefers an sRGB swapchain format so the hardware performs
// the linear->display gamma encode on write (the PBR shader outputs linear).
// Falls back to the surface's preferred format if none is sRGB.
func pickSurfaceFormat(formats []wgpu.TextureFormat) wgpu.TextureFormat {
	for _, f := range formats {
		if f == wgpu.TextureFormatBGRA8UnormSrgb || f == wgpu.TextureFormatRGBA8UnormSrgb {
			return f
		}
	}
	return formats[0]
}

// containsPresentMode reports whether mode is supported by the surface.
func containsPresentMode(modes []wgpu.PresentMode, mode wgpu.PresentMode) bool {
	for _, mm := range modes {
		if mm == mode {
			return true
		}
	}
	return false
}

// releaseGPU tears down every GPU object in reverse dependency order, then hands
// off to platformShutdown for window/DOM teardown. Safe to call once on exit
// (including on panic via AddOnExit).
func releaseGPU(g *GPU) {
	g.meshes.releaseAll()
	g.materials.releaseAll()
	if g.gizmo != nil {
		g.gizmo.release()
	}
	if g.overlay != nil {
		g.overlay.release()
	}
	if g.depthView != nil {
		g.depthView.Release()
	}
	if g.depthTex != nil {
		g.depthTex.Release()
	}
	if g.msaaView != nil {
		g.msaaView.Release()
	}
	if g.msaaTex != nil {
		g.msaaTex.Release()
	}
	if g.frameBindGroup != nil {
		g.frameBindGroup.Release()
	}
	if g.pointBuf != nil {
		g.pointBuf.Release()
	}
	if g.frameBuf != nil {
		g.frameBuf.Release()
	}
	if g.instanceBuf != nil {
		g.instanceBuf.Release()
	}
	if g.transparentPipeline != nil {
		g.transparentPipeline.Release()
	}
	if g.pipeline != nil {
		g.pipeline.Release()
	}
	if g.materialBGL != nil {
		g.materialBGL.Release()
	}
	if g.frameBGL != nil {
		g.frameBGL.Release()
	}
	if g.queue != nil {
		g.queue.Release()
	}
	if g.device != nil {
		g.device.Release()
	}
	if g.adapter != nil {
		g.adapter.Release()
	}
	if g.surface != nil {
		g.surface.Release()
	}
	if g.instance != nil {
		g.instance.Release()
	}
	g.platformShutdown()
}

// Size returns the current framebuffer size in pixels, or 0,0 if the GPU
// resource is not ready. This is the native GetFramebufferSize equivalent and
// works on both native and browser builds.
func Size(c *app.Ctx) (int, int) {
	g := app.GetResource[GPU](c)
	if g == nil {
		return 0, 0
	}
	return int(g.config.Width), int(g.config.Height)
}

// WindowSize returns the logical (screen-coordinate / CSS-pixel) window size —
// the space mouse-event coordinates live in. On HiDPI this is smaller than Size.
// Portable replacement for the native window's GetSize. Returns 0,0 if not ready.
func WindowSize(c *app.Ctx) (int, int) {
	g := app.GetResource[GPU](c)
	if g == nil {
		return 0, 0
	}
	return g.windowSize()
}

// CursorPos returns the latest cursor position in window (screen) coordinates,
// the same space as mouse events. Portable replacement for the native window's
// GetCursorPos.
func CursorPos(c *app.Ctx) (float64, float64) {
	g := app.GetResource[GPU](c)
	if g == nil {
		return 0, 0
	}
	return g.cursorX, g.cursorY
}

// KeyDown reports whether key is currently held, tracked from key events. This
// is the portable replacement for polling the native window's GetKey, and works
// the same on native and in the browser.
func KeyDown(c *app.Ctx, key inputs.Key) bool {
	g := app.GetResource[GPU](c)
	if g == nil {
		return false
	}
	return g.keysDown[key]
}
