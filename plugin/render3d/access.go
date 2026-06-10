package render3d

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
)

// These accessors expose the minimum of the otherwise-opaque GPU resource that a
// sibling overlay plugin (e.g. plugin/ui, the Dear ImGui backend) needs to build
// its own pipeline and textures and to record draws into the open frame. They
// mirror how overlay2d.go reaches into GPU from inside the package; external
// callers get read-only handles, not ownership.

// Device returns the wgpu device, or nil before Build. Use it to create
// pipelines, buffers, textures, and bind groups that draw into render3d's frame.
func Device(c *app.Ctx) *wgpu.Device {
	g := app.GetResource[GPU](c)
	if g == nil {
		return nil
	}
	return g.device
}

// Queue returns the wgpu queue, or nil before Build. Use it to upload buffer and
// texture data for overlay draws.
func Queue(c *app.Ctx) *wgpu.Queue {
	g := app.GetResource[GPU](c)
	if g == nil {
		return nil
	}
	return g.queue
}

// SurfaceFormat returns the swapchain color format. An overlay pipeline's color
// target must use this format to be compatible with the open render pass.
func SurfaceFormat(c *app.Ctx) wgpu.TextureFormat {
	g := app.GetResource[GPU](c)
	if g == nil {
		return wgpu.TextureFormatUndefined
	}
	return g.format
}

// DepthFormat returns the depth-stencil attachment format. An overlay pipeline
// must declare this format (typically depth-test Always, depth-write off) to be
// compatible with the open render pass.
func DepthFormat(c *app.Ctx) wgpu.TextureFormat {
	g := app.GetResource[GPU](c)
	if g == nil {
		return wgpu.TextureFormatUndefined
	}
	return g.depthFormat
}

// Samples returns the MSAA sample count of the render pass. An overlay pipeline's
// multisample state must match this to be pipeline-compatible.
func Samples(c *app.Ctx) uint32 {
	g := app.GetResource[GPU](c)
	if g == nil {
		return 1
	}
	return g.samples
}

// AddAfterInput registers a system in schedule.First that runs after the input
// poll, so it observes this frame's input events (KeyEvent/MouseMoveEvent/
// MouseButtonEvent) before the Update stage. An immediate-mode UI uses this to
// feed input into its IO and begin its frame ahead of user widget code.
func AddAfterInput(a *app.App, sys app.SystemFunc) {
	a.AddSystems(schedule.First, app.System(sys).After(inputLabel))
}
