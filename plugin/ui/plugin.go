// Package ui is a Dear ImGui (via cimgui-go) immediate-mode GUI for spliti,
// rendered on the GPU on top of a render3d frame. It replaces the CPU-rasterized
// image-panel approach (plugin/render3d/overlay2d) for structured, interactive
// UI: each frame ImGui builds a layout + draw-command tree that this package's
// wgpu backend records into render3d's open render pass, with first-class
// hover/click/double-click/drag/keyboard handling.
//
// Usage: add render3d.Plugin{} then ui.Plugin{} to the app. In ordinary Update
// systems, call cimgui-go widgets directly:
//
//	import imgui "github.com/AllenDang/cimgui-go/imgui"
//	func myUI(c *app.Ctx) {
//	    imgui.Begin("panel")
//	    if imgui.Button("play") { ... }
//	    imgui.End()
//	}
//
// Gate game-world input on ui.WantCaptureMouse / ui.WantCaptureKeyboard so the
// camera doesn't react while the pointer or focus is over the UI.
//
// Build requirements: cimgui-go is a cgo wrapper around the Dear ImGui C++
// library, so (like render3d) this builds only with CGO_ENABLED=1 and a C++
// toolchain.

//go:build !js

package ui

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	splititime "github.com/hvuhsg/spliti/plugin/time"
)

// Plugin installs the ImGui context, the wgpu render backend, and the per-frame
// begin/render systems. Add it after render3d.Plugin{}.
type Plugin struct {
	// Scale multiplies the UI's fonts and widget metrics. Zero (the default) auto-
	// detects the display content scale (e.g. 2 on a Retina display) so the UI is
	// the right physical size; set it explicitly to zoom further.
	Scale float32
}

// Build implements app.Plugin.
func (p Plugin) Build(a *app.App) {
	imgui.CreateContext()
	io := imgui.CurrentIO()
	io.SetIniFilename("") // don't persist window layout to an imgui.ini file
	io.SetConfigFlags(imgui.ConfigFlagsNavEnableKeyboard)
	// This backend uploads ImGui-managed textures on demand and uses per-draw
	// base-vertex offsets; advertise both so ImGui drives the modern paths.
	io.SetBackendFlags(imgui.BackendFlagsRendererHasTextures | imgui.BackendFlagsRendererHasVtxOffset)

	b := newBackend()
	b.wantScale = p.Scale
	app.InsertResource(a, b)

	render3d.AddAfterInput(a, beginFrame)
	render3d.AddOverlay(a, endFrameRender)

	a.AddOnExit(func() {
		b.release()
		imgui.DestroyContext()
	})
}

// beginFrame runs in schedule.First after the input poll: it sizes the display,
// advances ImGui's clock, feeds this frame's input, and opens the ImGui frame so
// Update-stage systems can issue widget calls.
func beginFrame(c *app.Ctx) {
	io := imgui.CurrentIO()
	applyScale(c)

	fbW, fbH := render3d.Size(c)
	if fbW <= 0 || fbH <= 0 {
		fbW, fbH = 1, 1
	}
	io.SetDisplaySize(imgui.Vec2{X: float32(fbW), Y: float32(fbH)})
	io.SetDisplayFramebufferScale(imgui.Vec2{X: 1, Y: 1})

	dt := float32(1.0 / 60.0)
	if t := app.GetResource[splititime.Time](c); t != nil {
		if s := float32(t.Delta().Seconds()); s > 0 {
			dt = s
		}
	}
	io.SetDeltaTime(dt)

	feedInput(c, io)
	imgui.NewFrame()
}

// applyScale sets the UI font + widget-metric scale once, from the plugin's
// configured scale or (when zero) the window's content scale. With ImGui's
// dynamic fonts this rasterizes crisply at the scaled size.
func applyScale(c *app.Ctx) {
	b := app.GetResource[Backend](c)
	if b == nil || b.scaled {
		return
	}
	s := b.wantScale
	if s <= 0 {
		if win := render3d.Window(c); win != nil {
			if sx, _ := win.GetContentScale(); sx > 0 {
				s = sx
			}
		}
	}
	if s <= 0 {
		s = 1
	}
	style := imgui.CurrentStyle()
	style.ScaleAllSizes(s)
	style.SetFontScaleDpi(s)
	b.uiScale = s
	b.scaled = true
}

// endFrameRender runs as a render3d overlay (after the 3D pass, before present):
// it closes the ImGui frame and records the resulting draw data into the open
// render pass. It always calls Render so the ImGui frame stays balanced even on
// frames where the surface was unavailable and nothing is recorded.
func endFrameRender(c *app.Ctx) {
	imgui.Render()
	b := app.GetResource[Backend](c)
	if b == nil {
		return
	}
	b.render(c, imgui.CurrentDrawData())
}
