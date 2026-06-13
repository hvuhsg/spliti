package editor

import (
	"sort"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
)

// prefabDragType tags ImGui drag payloads from the Assets panel; the dragged
// prefab name lives in state.dragPrefab.
const prefabDragType = "spliti-prefab"

// drawAssets lists the project's prefabs (//spliti:entity functions, wired in
// by spliti gen). Drag one into the Scene viewport to spawn an instance where
// it lands; double-click spawns at the origin.
func drawAssets(c *app.Ctx, st *state) {
	imgui.Begin("Assets")
	defer imgui.End()

	if len(st.cfg.Prefabs) == 0 {
		imgui.TextDisabled("no prefabs (game/entities)")
		return
	}
	names := make([]string, 0, len(st.cfg.Prefabs))
	for name := range st.cfg.Prefabs {
		names = append(names, name)
	}
	sort.Strings(names)

	imgui.TextDisabled("prefabs - drag into the scene")
	imgui.Separator()
	for _, name := range names {
		label := prefabLabel(name)
		imgui.SelectableBool(label)
		if imgui.BeginDragDropSource() {
			st.dragPrefab = name
			imgui.SetDragDropPayload(prefabDragType, dummyPayload(), 1)
			imgui.TextUnformatted(label)
			imgui.EndDragDropSource()
		}
		if imgui.IsItemHovered() && imgui.IsMouseDoubleClicked(imgui.MouseButtonLeft) {
			st.spawnPrefab(c, name, spawnPointFallback)
		}
	}
}
