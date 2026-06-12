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
	Scene    string  `json:"scene"`
	Selected string  `json:"selected,omitempty"`
	Camera   sessCam `json:"camera"`
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
	}
	if st.hasSelected {
		s.Selected = instanceName(c, st.selected)
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
	if s.Selected != "" {
		if e, ok := entityByInstance(c, s.Selected); ok {
			st.selected, st.hasSelected = e, true
		}
	}
}
