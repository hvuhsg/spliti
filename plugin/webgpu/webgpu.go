// Package webgpu is a GPU-backed render backend for spliti: it opens a window
// and draws textured-sprite entities through the GPU, as an alternative to the
// terminal renderer in plugin/tui.
//
// It is a second proof of spliti's render-as-a-plugin seam. Like terminal+tui,
// it owns no game logic — it installs a *GPU resource at Build and registers
// render/present systems in PostUpdate plus an input poll in First. spliti keeps
// driving app.Run() and the schedule; this plugin never takes over the loop
// (unlike Ebitengine-style frameworks).
//
// Entities render when they have Transform + Sprite (Color and Layer optional).
// Sprite.Ref names a texture uploaded into the TextureRegistry resource, e.g.
// from a Startup system. Components here are deliberately tcell-free.
//
// Build requirements: this backend uses cogentcore/webgpu (links a bundled
// wgpu-native static library) and go-gl/glfw (a cgo window binding), so it
// builds only with CGO_ENABLED=1 and a C toolchain (on macOS, the Xcode
// command-line tools). The terminal backend stays pure Go — importing this
// package is what pulls in the native deps.
//
// Threading: GLFW window and event calls must run on the OS thread that called
// glfw.Init. Build calls runtime.LockOSThread, and both AddPlugins (which runs
// Build) and Run must execute on the same (main) goroutine — the normal spliti
// usage. The demo's main package also calls runtime.LockOSThread in init() to
// make the requirement explicit.
package webgpu

import (
	"runtime"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/cogentcore/webgpu/wgpuglfw"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
)

// Plugin installs the GPU window, renderer, and input poll.
type Plugin struct {
	Width, Height int    // initial window size in pixels (defaults 800x600)
	Title         string // window title (defaults "spliti")

	// WorldW, WorldH define the visible world rectangle mapped to the window.
	// Zero means "use the framebuffer pixel size" (one world unit == one pixel).
	WorldW, WorldH float32

	// ClearColor fills the framebuffer each frame. Zero value is opaque black.
	ClearColor Color

	// VSync caps presentation to the display refresh (FIFO). When false the
	// renderer presents as fast as the surface allows (Immediate), falling back
	// to FIFO if the platform lacks it.
	VSync bool

	// Samples requests multisample anti-aliasing (MSAA), smoothing sprite-quad
	// edges. WebGPU portably supports 1 (off) and 4; any value >1 is treated as
	// 4. Note MSAA only anti-aliases geometry edges, so axis-aligned, integer-
	// positioned pixel-art sprites see little change — it pays off for rotated,
	// non-integer-scaled, or camera-zoomed content. Zero/1 leaves MSAA off.
	Samples int

	// Smooth switches texture sampling from Nearest (crisp pixel art, the
	// default) to Linear, and additionally to trilinear + anisotropic filtering
	// over the texture's mip chain. This anti-aliases ordinary sprite edges —
	// whose shape lives in the texture's alpha — both when magnified (linear) and
	// minified (mip levels stop scaled-down sprites from shimmering). It needs no
	// MSAA target, so it's far cheaper than Samples for that purpose. Independent
	// of Samples: the two smooth different edges (texture alpha vs. quad geometry)
	// and compose freely. (Mip levels are generated for every texture regardless;
	// Smooth only controls whether sampling across them is linear or nearest.)
	Smooth bool
}

// GPU is the shared rendering resource. Systems read it via
// app.GetResource[GPU]. All fields are owned by the plugin; treat as opaque.
type GPU struct {
	window   *glfw.Window
	instance *wgpu.Instance
	adapter  *wgpu.Adapter
	surface  *wgpu.Surface
	device   *wgpu.Device
	queue    *wgpu.Queue
	config   *wgpu.SurfaceConfiguration

	pipeline *wgpu.RenderPipeline
	sampler  *wgpu.Sampler
	// format is the swapchain/pipeline color format, kept so the pipeline can be
	// rebuilt (e.g. the MSAA fallback) without re-deriving it.
	format wgpu.TextureFormat

	// samples is the MSAA sample count baked into the pipeline (1 = off). When
	// >1 the render pass draws into msaaTex and resolves to the swapchain.
	samples  uint32
	msaaTex  *wgpu.Texture
	msaaView *wgpu.TextureView

	// smooth selects Linear texture filtering (vs. Nearest) for the sampler.
	smooth bool

	quadBuf     *wgpu.Buffer
	instanceBuf *wgpu.Buffer
	instanceCap int // capacity of instanceBuf, in instances

	cameraBuf       *wgpu.Buffer
	cameraBindGroup *wgpu.BindGroup
	camBGL          *wgpu.BindGroupLayout
	texBGL          *wgpu.BindGroupLayout

	registry *TextureRegistry // back-reference so teardown frees textures

	// pixelCamera is true when the Camera was left at its framebuffer-pixel
	// default (Plugin.WorldW/H == 0). Such a camera auto-refits on resize; a
	// game-set world rect is left untouched.
	pixelCamera bool

	clearColor wgpu.Color

	// Resize handshake: the framebuffer-size callback (main thread) sets these;
	// the input system applies them before the next frame.
	pendingW, pendingH int
	resized            bool

	// Input buffers filled by GLFW callbacks during PollEvents and drained by
	// the input system in the same (main) goroutine — no locking needed.
	keyEvents []KeyEvent

	// Reusable per-frame scratch, like tui's reused layered buffer: collected
	// renderables, packed instance data (in draw order), and texture batches.
	items   []renderItem
	scratch []instanceData
	batches []spriteBatch

	// Open frame state, shared between the render and present systems (both run
	// on the main goroutine in PostUpdate). frameActive is false when the
	// surface texture couldn't be acquired and the frame must be skipped.
	curTex      *wgpu.Texture
	curView     *wgpu.TextureView
	curEncoder  *wgpu.CommandEncoder
	curPass     *wgpu.RenderPassEncoder
	frameActive bool
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	runtime.LockOSThread()

	width, height := p.Width, p.Height
	if width <= 0 {
		width = 800
	}
	if height <= 0 {
		height = 600
	}
	title := p.Title
	if title == "" {
		title = "spliti"
	}

	if err := glfw.Init(); err != nil {
		panic("webgpu: glfw init: " + err.Error())
	}
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI) // wgpu owns the surface, not GL
	glfw.WindowHint(glfw.Resizable, glfw.True)
	win, err := glfw.CreateWindow(width, height, title, nil, nil)
	if err != nil {
		glfw.Terminate()
		panic("webgpu: create window: " + err.Error())
	}

	instance := wgpu.CreateInstance(nil)
	surface := instance.CreateSurface(wgpuglfw.GetSurfaceDescriptor(win))

	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{
		CompatibleSurface: surface,
	})
	if err != nil {
		panic("webgpu: request adapter: " + err.Error())
	}
	device, err := adapter.RequestDevice(nil)
	if err != nil {
		panic("webgpu: request device: " + err.Error())
	}
	queue := device.GetQueue()

	// Configure the surface at the real framebuffer size in pixels — on HiDPI
	// this is larger than the window's screen-coordinate size, giving crisp
	// rendering at native resolution.
	fbw, fbh := win.GetFramebufferSize()
	caps := surface.GetCapabilities(adapter)
	present := wgpu.PresentModeFifo
	if !p.VSync && containsPresentMode(caps.PresentModes, wgpu.PresentModeImmediate) {
		present = wgpu.PresentModeImmediate
	}
	config := &wgpu.SurfaceConfiguration{
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      caps.Formats[0],
		Width:       uint32(fbw),
		Height:      uint32(fbh),
		PresentMode: present,
		AlphaMode:   caps.AlphaModes[0],
	}
	surface.Configure(adapter, device, config)

	g := &GPU{
		window:   win,
		instance: instance,
		adapter:  adapter,
		surface:  surface,
		device:   device,
		queue:    queue,
		config:   config,
		samples:  normalizeSamples(p.Samples),
		smooth:   p.Smooth,
		clearColor: wgpu.Color{
			R: float64(p.ClearColor.R),
			G: float64(p.ClearColor.G),
			B: float64(p.ClearColor.B),
			A: float64(p.ClearColor.orDefault().A),
		},
	}

	// Pipeline, sampler, buffers, camera bind group.
	buildPipeline(g, config.Format)

	// MSAA color target (no-op when Samples <= 1). Recreated on resize.
	ensureMSAATarget(g)

	// Camera: default the world rect to the framebuffer pixel size.
	cam := &Camera{WorldW: p.WorldW, WorldH: p.WorldH}
	if p.WorldW <= 0 || p.WorldH <= 0 {
		g.pixelCamera = true
		cam.WorldW = float32(fbw)
		cam.WorldH = float32(fbh)
	}
	writeCamera(g, cam)

	registry := newTextureRegistry(g)
	g.registry = registry

	app.InsertResource(a, g)
	app.InsertResource(a, cam)
	app.InsertResource(a, registry)

	// GLFW callbacks run on the main thread during PollEvents (in the input
	// system), so appending here races nothing.
	win.SetFramebufferSizeCallback(func(_ *glfw.Window, w, h int) {
		g.pendingW, g.pendingH = w, h
		g.resized = true
	})
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, mods glfw.ModifierKey) {
		g.keyEvents = append(g.keyEvents, KeyEvent{Key: key, Action: action, Mods: mods})
	})
	win.SetCharCallback(func(_ *glfw.Window, r rune) {
		g.keyEvents = append(g.keyEvents, KeyEvent{Rune: r, Action: glfw.Press})
	})

	a.AddOnExit(func() { releaseGPU(g) })

	installSystems(a)
}

// containsPresentMode reports whether mode is supported by the surface.
func containsPresentMode(modes []wgpu.PresentMode, mode wgpu.PresentMode) bool {
	for _, m := range modes {
		if m == mode {
			return true
		}
	}
	return false
}

// writeCamera uploads the ortho matrix for cam to the camera uniform buffer.
func writeCamera(g *GPU, cam *Camera) {
	m := ortho(cam.WorldW, cam.WorldH)
	_ = g.queue.WriteBuffer(g.cameraBuf, 0, wgpu.ToBytes(m[:]))
}

// releaseGPU tears down every GPU object in reverse dependency order and shuts
// GLFW down. Safe to call once on exit (including on panic via AddOnExit).
func releaseGPU(g *GPU) {
	g.registry.releaseAll()
	if g.msaaView != nil {
		g.msaaView.Release()
	}
	if g.msaaTex != nil {
		g.msaaTex.Release()
	}
	if g.cameraBindGroup != nil {
		g.cameraBindGroup.Release()
	}
	if g.cameraBuf != nil {
		g.cameraBuf.Release()
	}
	if g.instanceBuf != nil {
		g.instanceBuf.Release()
	}
	if g.quadBuf != nil {
		g.quadBuf.Release()
	}
	if g.sampler != nil {
		g.sampler.Release()
	}
	if g.pipeline != nil {
		g.pipeline.Release()
	}
	if g.texBGL != nil {
		g.texBGL.Release()
	}
	if g.camBGL != nil {
		g.camBGL.Release()
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
	if g.window != nil {
		g.window.Destroy()
	}
	glfw.Terminate()
}

// Window returns the GLFW window, or nil before Build. Convenience for systems
// that need direct window access.
func Window(c *app.Ctx) *glfw.Window {
	g := app.GetResource[GPU](c)
	if g == nil {
		return nil
	}
	return g.window
}

// Size returns the current framebuffer size in pixels, or 0,0 if the GPU
// resource is not ready.
func Size(c *app.Ctx) (int, int) {
	g := app.GetResource[GPU](c)
	if g == nil {
		return 0, 0
	}
	return int(g.config.Width), int(g.config.Height)
}
