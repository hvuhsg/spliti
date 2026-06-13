package editor

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/hvuhsg/spliti/app"
)

// sessionData is what survives an editor restart (rebuild & re-exec, or just
// quitting): the camera pose and the selection. It lives next to the ImGui
// layout under .spliti/.
type sessionData struct {
	Scene string `json:"scene"`
	// Selected is the legacy single-selection field; Selection supersedes it
	// (ordered, last entry is the primary).
	Selected  string   `json:"selected,omitempty"`
	Selection []string `json:"selection,omitempty"`
	Camera    sessCam  `json:"camera"`
	View      sessView `json:"view"`
}

// sessView persists the editor's look settings (View menu). A pointer-free
// value type so a missing "view" key restores as the zero value, which
// restoreSession treats as "keep the defaults".
type sessView struct {
	Theme     int     `json:"theme"`
	FontScale float32 `json:"fontScale"`
	ShowGrid  bool    `json:"showGrid"`
}

type sessCam struct {
	Pivot [3]float32 `json:"pivot"`
	Dist  float32    `json:"dist"`
	Yaw   float32    `json:"yaw"`
	Pitch float32    `json:"pitch"`
}

func (st *state) sessionPath() string {
	return filepath.Join(st.cfg.ProjectRoot, ".spliti", "session.json")
}

// saveSession runs on exit and before a rebuild re-exec; failures are
// non-fatal (the session is a convenience).
func (st *state) saveSession(c *app.Ctx) {
	s := sessionData{
		Scene: st.cfg.Scene,
		Camera: sessCam{
			Pivot: [3]float32{st.cam.pivot.X, st.cam.pivot.Y, st.cam.pivot.Z},
			Dist:  st.cam.dist,
			Yaw:   st.cam.yaw,
			Pitch: st.cam.pitch,
		},
		View: sessView{
			Theme:     st.view.themeIdx,
			FontScale: clampFontScale(st.view.fontScale),
			ShowGrid:  st.view.showGrid,
		},
	}
	for _, e := range st.sel {
		if n := instanceName(c, e); n != "" {
			s.Selection = append(s.Selection, n)
		}
	}
	if len(s.Selection) > 0 {
		s.Selected = s.Selection[len(s.Selection)-1]
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(st.sessionPath()), 0o755)
	os.WriteFile(st.sessionPath(), data, 0o644)
}

// restoreSession applies a saved session after the scene is set up. The
// selection is resolved by instance name (entity handles never persist).
func (st *state) restoreSession(c *app.Ctx) {
	data, err := os.ReadFile(st.sessionPath())
	if err != nil {
		return
	}
	var s sessionData
	if err := json.Unmarshal(data, &s); err != nil || s.Scene != st.cfg.Scene {
		return
	}
	if s.Camera.Dist > 0 {
		st.cam.pivot.X, st.cam.pivot.Y, st.cam.pivot.Z = s.Camera.Pivot[0], s.Camera.Pivot[1], s.Camera.Pivot[2]
		st.cam.dist = s.Camera.Dist
		st.cam.yaw, st.cam.pitch = s.Camera.Yaw, s.Camera.Pitch
	}
	// FontScale is the presence sentinel: a session predating the View menu has
	// no "view" key, so it unmarshals to the zero value (which would mean grid
	// off, zero font) — keep the defaults in that case.
	if s.View.FontScale > 0 {
		st.view.themeIdx = s.View.Theme
		st.view.fontScale = clampFontScale(s.View.FontScale)
		st.view.showGrid = s.View.ShowGrid
		st.view.theme() // repair an out-of-range theme index
	}
	sel := s.Selection
	if len(sel) == 0 && s.Selected != "" {
		sel = []string{s.Selected}
	}
	for _, name := range sel {
		if e, ok := entityByInstance(c, name); ok && !st.isSelected(e) {
			st.sel = append(st.sel, e)
		}
	}
}
