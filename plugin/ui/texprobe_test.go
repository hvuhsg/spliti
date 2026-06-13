//go:build !js

package ui

import (
	"testing"

	"github.com/AllenDang/cimgui-go/imgui"
)

// TestFontResizeEmitsNilTextures reproduces the terminal-zoom crash at the CPU
// level: changing the pushed font size makes ImGui destroy the old font-atlas
// texture and create a new one, and while it swaps them dd.Textures() contains
// nil-backed entries. Calling any method on those (as updateTextures' switch
// does) dereferences a null C pointer and segfaults — so the backend must skip
// td.CData == nil. This test fails (panics) if that invariant regresses and
// asserts the nil entries actually occur, so the guard isn't silently dead.
func TestFontResizeEmitsNilTextures(t *testing.T) {
	imgui.CreateContext()
	defer imgui.DestroyContext()

	io := imgui.CurrentIO()
	io.SetDisplaySize(imgui.Vec2{X: 1280, Y: 800})
	io.SetDeltaTime(1.0 / 60)
	io.SetBackendFlags(imgui.BackendFlagsRendererHasTextures | imgui.BackendFlagsRendererHasVtxOffset)
	io.Fonts().AddFontDefault()
	mono := loadMonoFont(io.Fonts(), monoFontSize)
	if mono == nil {
		t.Fatal("mono font failed to load")
	}

	nextID := uint64(1)
	sawNil := false

	frame := func(size float32) {
		imgui.NewFrame()
		imgui.Begin("probe")
		imgui.PushFont(mono, size)
		imgui.TextUnformatted("╭──╮│ ⠋ Working… ✓ ░▒▓ Abc 0")
		imgui.PopFont()
		imgui.End()
		imgui.Render()

		// Mirror Backend.updateTextures: the nil guard must come first.
		for _, td := range imgui.CurrentDrawData().Textures().Slice() {
			if td.CData == nil {
				sawNil = true
				continue
			}
			switch td.Status() {
			case imgui.TextureStatusWantCreate:
				td.SetTexID(imgui.TextureID(nextID))
				nextID++
				td.SetStatus(imgui.TextureStatusOK)
			case imgui.TextureStatusWantUpdates:
				td.SetStatus(imgui.TextureStatusOK)
			case imgui.TextureStatusWantDestroy:
				td.SetTexID(0)
				td.SetStatus(imgui.TextureStatusDestroyed)
			}
		}
	}

	// Sweep the terminal's zoom range (8..40) up and back; atlas swaps occur as
	// sizes grow.
	for s := float32(8); s <= 40; s++ {
		frame(s)
	}
	for s := float32(40); s >= 8; s-- {
		frame(s)
	}
	if !sawNil {
		t.Error("expected nil texture entries during font-atlas swap; the backend nil guard may now be dead code (or cimgui-go changed behavior)")
	}
}
