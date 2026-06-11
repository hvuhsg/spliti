// Package spatial3d ties audio's 3D spatial voices to render3d's scene: the
// active Camera3D becomes the listener and entities carrying an Emitter feed
// their world position into their voice every frame.
//
// It is a separate package so plugin/audio stays free of GPU dependencies —
// only games that already use render3d add this plugin:
//
//	a.AddPlugins(render3d.Plugin{}, audio.Plugin{}, spatial3d.Plugin{})
//
//	// start a positioned voice and attach it to an entity:
//	h := au.PlayAt3D("engine", pos, audio.PlayOptions{Loop: true}, audio.SpatialOptions{MaxDist: 30})
//	... add spatial3d.Emitter{Handle: h} to the entity ...
package spatial3d

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/audio"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
)

// Emitter ties a playing spatial voice to an entity. The sync system pushes
// the entity's GlobalTransform translation into the voice each frame, and
// resets Handle to NoHandle once the voice ends.
type Emitter struct {
	Handle audio.Handle
}

// Plugin installs the per-frame listener/emitter sync. It requires both
// audio.Plugin and render3d.Plugin to be added first.
type Plugin struct{}

// Build implements app.Plugin.
func (Plugin) Build(a *app.App) {
	if app.GetResource[audio.Audio](a.Ctx()) == nil {
		panic("spatial3d.Plugin requires audio.Plugin to be added first")
	}
	if app.GetResource[render3d.Camera3D](a.Ctx()) == nil {
		panic("spatial3d.Plugin requires render3d.Plugin to be added first")
	}
	// After transform propagation so emitters read this frame's world
	// positions (parent chains included).
	a.AddSystems(schedule.PostUpdate,
		app.System(sync).Label("__spliti_audio_spatial3d").After("__render3d_transform"))
}

func sync(c *app.Ctx) {
	au := app.GetResource[audio.Audio](c)
	cam := app.GetResource[render3d.Camera3D](c)
	au.SetListener3D(cam.Position, cam.Target.Sub(cam.Position), cam.Up)

	app.Query2[render3d.GlobalTransform, Emitter](c, func(_ ecs.Entity, gt *render3d.GlobalTransform, em *Emitter) {
		if em.Handle == audio.NoHandle {
			return
		}
		if !au.IsPlaying(em.Handle) {
			em.Handle = audio.NoHandle
			return
		}
		// World position is the translation column of the global matrix.
		au.SetVoicePos3D(em.Handle, m.Vec3{X: gt.Matrix[12], Y: gt.Matrix[13], Z: gt.Matrix[14]})
	})
}
