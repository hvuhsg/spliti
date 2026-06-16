package check

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/schedule"
)

// Script is a deterministic, frame-indexed input timeline. Frames are 0-based:
// frame 0 is the first iteration of the run loop. It emits raw key events that
// the input layer (plugin/inputs/actions) turns into held/edge action state, so
// scripted runs exercise the same input path as a real player.
//
//	s := check.NewScript().Hold(inputs.KeyD, 0, 5).Press(8, inputs.KeySpace)
//
// Held state persists between a Press and its matching Release, exactly as
// physical keys do.
type Script struct {
	presses  map[int][]inputs.Key
	releases map[int][]inputs.Key
}

// NewScript returns an empty Script.
func NewScript() *Script {
	return &Script{presses: map[int][]inputs.Key{}, releases: map[int][]inputs.Key{}}
}

// Press emits a key-down at the given frame. Without a matching Release the key
// stays held for the rest of the run.
func (s *Script) Press(frame int, k inputs.Key) *Script {
	s.presses[frame] = append(s.presses[frame], k)
	return s
}

// Release emits a key-up at the given frame.
func (s *Script) Release(frame int, k inputs.Key) *Script {
	s.releases[frame] = append(s.releases[frame], k)
	return s
}

// Hold presses k at frame from and releases it at frame to (held during
// [from, to)).
func (s *Script) Hold(k inputs.Key, from, to int) *Script {
	return s.Press(from, k).Release(to, k)
}

// install adds the First-stage emitter. It runs before PreUpdate (where the
// action map reads the frame's events), so a key pressed at frame N is held for
// that frame's systems.
func (s *Script) install(a *app.App) {
	a.AddSystems(schedule.First, app.System(func(c *app.Ctx) {
		f := a.Frame()
		for _, k := range s.presses[f] {
			app.SendEvent(c, inputs.KeyEvent{Key: k, Action: inputs.Press})
		}
		for _, k := range s.releases[f] {
			app.SendEvent(c, inputs.KeyEvent{Key: k, Action: inputs.Release})
		}
	}).Label("__spliti_check_script"))
}
