package editor

import (
	"fmt"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
)

// drawSystems lists the game systems recorded by the registration interceptor,
// grouped by stage, each with a runtime enable toggle. The toggles are session
// state only — they gate execution in Play mode but write nothing to source.
func drawSystems(c *app.Ctx, st *state) {
	if !imgui.Begin("Systems") {
		imgui.End()
		return
	}
	if len(st.gameSystems) == 0 {
		imgui.TextDisabled("no game systems registered")
		imgui.TextDisabled("(add them in game.RegisterSystems)")
		imgui.End()
		return
	}
	for _, stage := range stageOrder {
		var inStage []*gameSystem
		for _, gs := range st.gameSystems {
			if gs.stage == stage {
				inStage = append(inStage, gs)
			}
		}
		if len(inStage) == 0 {
			continue
		}
		imgui.SeparatorText(string(stage))
		for i, gs := range inStage {
			imgui.PushIDStr(fmt.Sprintf("%s-%d", gs.name, i))
			imgui.Checkbox(gs.name, &gs.enabled)
			imgui.PopID()
		}
	}
	imgui.End()
}

// stageOrder is the display order of the main-loop stages.
var stageOrder = []schedule.Stage{
	schedule.First,
	schedule.PreUpdate,
	schedule.StateTransition,
	schedule.FixedFirst,
	schedule.FixedUpdate,
	schedule.FixedLast,
	schedule.Update,
	schedule.PostUpdate,
	schedule.Last,
}
