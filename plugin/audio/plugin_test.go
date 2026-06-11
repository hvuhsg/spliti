package audio

import (
	"testing"
	"time"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
)

// TestPumpPublishesFinished runs the pump system inside a real App against a
// deviceless mixer (the null-sink path) and checks Finished events reach game
// systems as frame-buffered events.
func TestPumpPublishesFinished(t *testing.T) {
	a := app.New()
	mix := NewAudio(testRate, 4)
	app.InsertResource(a, mix)
	app.InsertResource(a, mix.reg)
	a.AddSystems(schedule.First, app.System(pumpSystem(mix, false)).Label("__spliti_audio_pump"))

	mustLoadPCM(t, mix, "shot", constPCM(0.5, 100), 1, testRate)

	var h Handle
	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		h = app.GetResource[Audio](c).Play("shot")
	})

	var got []Finished
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		// Drive the mixer manually (no device in tests); the one-shot ends
		// during the first frame's Advance.
		app.GetResource[Audio](c).Advance(50 * time.Millisecond)
		got = append(got, app.ReadEvents[Finished](c)...)
	})

	a.SetMaxFrames(3)
	a.Run()

	if len(got) != 1 || got[0].Handle != h || got[0].Ref != "shot" {
		t.Fatalf("Finished events = %+v, want exactly one for %v/shot", got, h)
	}
}
