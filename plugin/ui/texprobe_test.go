//go:build !js

package ui

import (
	"testing"

	"github.com/AllenDang/cimgui-go/imgui"
)

// TestTextureProtocol validates, headlessly, the texture-management protocol the
// backend uses across font-atlas swaps (terminal Cmd/Ctrl +/- zoom). It mirrors
// updateTextures' GPU-independent logic and then performs render's cmd.TexID()
// check, which panics (via ImGui's assert) if a drawn texture wasn't given a
// valid id. cimgui-go's DrawData.Textures()/TexList() only expose index 0
// correctly, so the protocol must drive creation from io.Fonts().TexData() (the
// authoritative current atlas texture) and track textures by UniqueID.
func TestTextureProtocol(t *testing.T) {
	imgui.CreateContext()
	defer imgui.DestroyContext()

	io := imgui.CurrentIO()
	io.SetIniFilename("") // don't write an imgui.ini in the package dir
	io.SetDisplaySize(imgui.Vec2{X: 1280, Y: 800})
	io.SetDeltaTime(1.0 / 60)
	io.SetBackendFlags(imgui.BackendFlagsRendererHasTextures | imgui.BackendFlagsRendererHasVtxOffset)
	io.Fonts().AddFontDefault()
	mono := loadMonoFont(io.Fonts(), monoFontSize)
	if mono == nil {
		t.Fatal("mono font failed to load")
	}

	// Simulated GPU-side registry, keyed by the id we hand ImGui, plus a uid->id
	// index and the ImTextureData handles so we can poll for safe destruction.
	type rec struct {
		id     imgui.TextureID
		handle imgui.TextureData
	}
	byUID := map[int32]rec{}
	live := map[imgui.TextureID]bool{} // "GPU textures" currently allocated
	next := uint64(1)
	maxLive := 0

	ensure := func(td *imgui.TextureData) {
		uid := td.UniqueID()
		r, ok := byUID[uid]
		if !ok {
			r = rec{id: imgui.TextureID(next), handle: *td}
			next++
			live[r.id] = true // createGPU
			byUID[uid] = r
		}
		if s := td.Status(); s == imgui.TextureStatusWantCreate || s == imgui.TextureStatusWantUpdates {
			// uploadGPU(r.id, td)
			td.SetStatus(imgui.TextureStatusOK)
		}
		td.SetTexID(r.id) // always (re)assert the id for the current texture
	}

	process := func(dd *imgui.DrawData) {
		if cur := io.Fonts().TexData(); cur != nil && cur.CData != nil {
			ensure(cur)
		}
		curUID := int32(-1)
		if cur := io.Fonts().TexData(); cur != nil && cur.CData != nil {
			curUID = cur.UniqueID()
		}
		for uid, r := range byUID {
			if uid == curUID || r.handle.CData == nil {
				continue
			}
			if r.handle.Status() == imgui.TextureStatusWantDestroy && r.handle.UnusedFrames() > 0 {
				delete(live, r.id) // releaseGPU
				r.handle.SetTexID(0)
				r.handle.SetStatus(imgui.TextureStatusDestroyed)
				delete(byUID, uid)
			}
		}
		if len(live) > maxLive {
			maxLive = len(live)
		}
		// render()'s per-command texture lookup asserts on an invalid id.
		for _, dl := range dd.CommandLists() {
			for _, cmd := range dl.Commands() {
				_ = cmd.TexID()
			}
		}
	}

	frame := func(size float32) {
		imgui.NewFrame()
		imgui.Begin("probe")
		imgui.PushFont(mono, size)
		imgui.TextUnformatted("╭──╮│ ⠋ Working… ✓ ░▒▓ Abc 0")
		imgui.PopFont()
		imgui.End()
		imgui.Render()
		process(imgui.CurrentDrawData())
	}

	// Several zoom round trips; would panic on a bad texid, and leak if old
	// atlas textures were never destroyed.
	for round := 0; round < 3; round++ {
		for s := float32(8); s <= 40; s++ {
			frame(s)
		}
		for s := float32(40); s >= 8; s-- {
			frame(s)
		}
	}

	t.Logf("max live textures: %d; tracked at end: %d", maxLive, len(byUID))
	if maxLive > 4 {
		t.Errorf("too many simultaneous textures (%d) — old atlases not being destroyed", maxLive)
	}
	if len(byUID) > 3 {
		t.Errorf("texture tracking grew unbounded: %d entries", len(byUID))
	}
}
