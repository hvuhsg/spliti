//go:build !js

package ui

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/hvuhsg/spliti/app"
)

// WantCaptureMouse reports whether ImGui wants the mouse this frame (the pointer
// is over a UI window/widget, or a UI drag is active). Game-world systems should
// skip mouse handling when this is true. Valid after the UI frame has begun
// (i.e. in Update systems running after ui.Plugin's begin-frame).
func WantCaptureMouse(*app.Ctx) bool {
	return imgui.CurrentIO().WantCaptureMouse()
}

// WantCaptureKeyboard reports whether ImGui wants keyboard input this frame (a
// text field is focused, etc.). Game-world systems should skip keyboard handling
// when this is true.
func WantCaptureKeyboard(*app.Ctx) bool {
	return imgui.CurrentIO().WantCaptureKeyboard()
}

// Scale returns the UI scale applied to fonts and widget metrics (1 until the
// first frame has run). Use it to size any custom framebuffer-pixel UI you draw
// alongside ImGui (e.g. a DrawList node canvas) so it scales with the rest.
func Scale(c *app.Ctx) float32 {
	b := app.GetResource[Backend](c)
	if b == nil || b.uiScale == 0 {
		return 1
	}
	return b.uiScale
}

// RegisterTexture makes an existing wgpu texture view drawable inside ImGui (via
// imgui.ImageWithBg / imgui.Image), returning a TextureID to pass as the image
// handle. The caller retains ownership of the view; the returned id stays valid
// until the backend is released. Returns 0 if the UI/GPU isn't ready yet.
func RegisterTexture(c *app.Ctx, view *wgpu.TextureView) imgui.TextureID {
	b := app.GetResource[Backend](c)
	if b == nil || view == nil || !b.ensureGPU(c) {
		return 0
	}
	bg, err := b.bgl1Group(c, view)
	if err != nil {
		return 0
	}
	id := imgui.TextureID(b.nextTexID)
	b.nextTexID++
	// view/tex are caller-owned, so the entry only owns its bind group.
	b.textures[id] = &texEntry{bg: bg}
	return id
}
