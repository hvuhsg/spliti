package editor

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/ui"
)

// drawGameView renders the Game panel: the world as the game's own camera
// sees it, through a render3d.SceneView (a second scene pass with no editor
// gizmo lines). The global Camera3D resource is that camera — scene setup
// poses it, and game systems drive it during Play. The panel is display-only:
// no picking, no gizmo, no navigation.
func drawGameView(c *app.Ctx, st *state) {
	imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, imgui.Vec2{})
	open := imgui.BeginV("Game", nil,
		imgui.WindowFlagsNoScrollbar|imgui.WindowFlagsNoScrollWithMouse)
	imgui.PopStyleVar()
	if !open {
		// Hidden tab or collapsed: skip the second scene pass entirely.
		if st.gameView != nil {
			st.gameView.SetEnabled(false)
		}
		imgui.End()
		return
	}

	gcam := app.GetResource[render3d.Camera3D](c)
	avail := imgui.ContentRegionAvail()
	w, h := int(avail.X), int(avail.Y)
	if gcam == nil || w < 16 || h < 16 {
		if st.gameView != nil {
			st.gameView.SetEnabled(false)
		}
		imgui.End()
		return
	}

	if st.gameRT == nil {
		st.gameRT = render3d.NewRenderTarget(c, w, h)
		if st.gameRT == nil {
			imgui.End()
			return
		}
		st.gameView = render3d.NewSceneView(c, st.gameRT, gcam)
		st.gameTexID = ui.RegisterTexture(c, st.gameRT.View())
	} else if rw, rh := st.gameRT.Size(); rw != w || rh != h {
		st.gameRT.Resize(c, w, h)
		ui.UpdateTexture(c, st.gameTexID, st.gameRT.View())
	}
	if st.gameView == nil {
		imgui.End()
		return
	}
	st.gameView.SetEnabled(true)
	// The panel owns the game camera's aspect while the editor is up (with a
	// scene target set, the window resize handler leaves aspect alone).
	gcam.SetAspect(float32(w) / float32(h))

	imgui.Image(*imgui.NewTextureRefTextureID(st.gameTexID), avail)
	imgui.End()
}
