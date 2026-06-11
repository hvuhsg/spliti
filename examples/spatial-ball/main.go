// Command spatial-ball is a tiny 3D scene that demonstrates positional audio:
// a glowing ball orbits the origin while continuously emitting a tone, and you
// fly around as the listener to hear the sound pan left/right and fade with
// distance. The ball carries a spatial3d.Emitter, so the audio plugin tracks
// its world position every frame and the active Camera3D is the listener.
//
// Controls:
//
//	W/A/S/D     move (horizontal), relative to where you look
//	Q/E         move down / up
//	right-drag  look around
//	Esc         quit
//
// Requires a GPU window, an audio device, cgo, and a C toolchain:
//
//	CGO_ENABLED=1 go run ./examples/spatial-ball
package main

import (
	"fmt"
	"math"
	"runtime"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/audio"
	"github.com/hvuhsg/spliti/plugin/audio/spatial3d"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// The GLFW window must live on the main OS thread.
func init() { runtime.LockOSThread() }

const (
	sampleRate  = 48000
	orbitRadius = 6.0  // metres from origin the ball circles at
	orbitHeight = 1.4  // ball height above the ground
	orbitSpeed  = 0.7  // radians/sec around the circle
	toneFreq    = 330  // Hz carrier of the emitted tone
)

// Ball tags the orbiting sound source and stores its angle around the circle.
type Ball struct{ Angle float32 }

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{TargetFrameRate: 0},
		render3d.Plugin{
			Width: 1280, Height: 720,
			Title:      "spliti — spatial-ball",
			ClearColor: render3d.Color{R: 0.02, G: 0.03, B: 0.05, A: 1},
			Ambient:    m.Vec3{X: 0.05, Y: 0.06, Z: 0.08},
			Samples:    4,
			VSync:      true,
		},
		audio.Plugin{SampleRate: sampleRate},
		spatial3d.Plugin{}, // active Camera3D = listener; Emitters track entity pos
	)

	app.InsertResource(a, &CamCtl{})

	a.AddSystems(schedule.Startup, setup)
	a.AddSystems(schedule.Update, controlsSystem)
	a.AddSystems(schedule.Update, moveBall)

	fmt.Println("spatial-ball: WASD/QE to move, right-drag to look, Esc to quit.")
	fmt.Println("Fly toward and around the glowing ball — the tone pans and fades with your position.")

	a.Run()
}

func setup(c *app.Ctx) {
	meshes := app.GetResource[render3d.MeshRegistry](c)
	meshes.Load("ball", render3d.UVSphere(0.5, 48, 32))
	meshes.Load("marker", render3d.UVSphere(0.12, 12, 8))
	meshes.Load("ground", render3d.Plane(40, 40, 1, 1))

	mats := app.GetResource[render3d.MaterialRegistry](c)
	mats.Load("ball", render3d.Material{
		BaseColor: render3d.Color{R: 1, G: 0.25, B: 0.2, A: 1},
		Emissive:  m.Vec3{X: 1.4, Y: 0.35, Z: 0.25}, // glows so it reads as the source
		Roughness: 0.4,
	})
	mats.Load("marker", render3d.Material{
		BaseColor: render3d.Color{R: 0.25, G: 0.3, B: 0.45, A: 1},
		Emissive:  m.Vec3{X: 0.08, Y: 0.1, Z: 0.18},
		Roughness: 0.8,
	})
	mats.Load("ground", render3d.Material{
		BaseColor: render3d.Color{R: 0.08, G: 0.09, B: 0.11, A: 1},
		Roughness: 0.95,
	})

	// Ground + a faint ring of markers tracing the ball's orbit, so you have
	// something to orient against while moving.
	render3d.SpawnMesh(c.Commands(), render3d.NewTransform3D(m.Vec3{}), "ground", "ground")
	const markers = 24
	for i := 0; i < markers; i++ {
		ang := float64(i) / markers * 2 * math.Pi
		pos := m.Vec3{X: orbitRadius * float32(math.Cos(ang)), Y: 0.12, Z: orbitRadius * float32(math.Sin(ang))}
		render3d.SpawnMesh(c.Commands(), render3d.NewTransform3D(pos), "marker", "marker")
	}

	render3d.SpawnDirectionalLight(c.Commands(), render3d.DirectionalLight{
		Direction: m.Vec3{X: -0.4, Y: -1, Z: -0.3}.Normalize(),
		Color:     m.Vec3{X: 1, Y: 0.98, Z: 0.95},
		Intensity: 2.2,
	})

	// Generate a seamless looping tone and start it as a 3D voice. PlayAt3D
	// returns immediately with a handle we hand to the ball's Emitter; the
	// spatial3d sync system then pins the voice to the ball's position.
	reg := app.GetResource[audio.Registry](c)
	if err := reg.LoadPCM("tone", loopTone(), 1, sampleRate); err != nil {
		panic(err)
	}
	au := app.GetResource[audio.Audio](c)
	start := orbitPos(0)
	h := au.PlayAt3D("tone", start,
		audio.PlayOptions{Loop: true, Volume: 0.9},
		audio.SpatialOptions{MinDist: 1.5, MaxDist: 22, Rolloff: audio.RolloffInverse},
	)

	// Spawn the ball with the full renderable component set plus the Emitter
	// and Ball tag. (SpawnMesh can't add our extra components, so build it
	// directly like the spawn helpers do.)
	c.Commands().Add(func(w *ecs.World) {
		mp := generic.NewMap6[
			render3d.Transform3D, render3d.GlobalTransform, render3d.MeshRenderer,
			render3d.MaterialRef, spatial3d.Emitter, Ball,
		](w)
		t := render3d.NewTransform3D(start)
		mp.NewWith(&t, &render3d.GlobalTransform{Matrix: m.Identity4()},
			&render3d.MeshRenderer{Mesh: "ball"}, &render3d.MaterialRef{Material: "ball"},
			&spatial3d.Emitter{Handle: h}, &Ball{})
	})
}

// moveBall advances the ball's angle and writes its orbit position into the
// Transform3D. The renderer propagates that to GlobalTransform later this frame,
// and the spatial3d Emitter sync (PostUpdate) feeds it to the voice.
func moveBall(c *app.Ctx) {
	tm := app.GetResource[splititime.Time](c)
	dt := float32(tm.Delta().Seconds())
	app.Query2[render3d.Transform3D, Ball](c, func(_ ecs.Entity, t *render3d.Transform3D, b *Ball) {
		b.Angle += dt * orbitSpeed
		t.Translation = orbitPos(b.Angle)
	})
}

func orbitPos(angle float32) m.Vec3 {
	return m.Vec3{
		X: orbitRadius * float32(math.Cos(float64(angle))),
		Y: orbitHeight,
		Z: orbitRadius * float32(math.Sin(float64(angle))),
	}
}

// loopTone builds one second of a gently throbbing sine tone. Both the carrier
// and the tremolo complete a whole number of cycles in the buffer, so the loop
// wraps seamlessly with no click.
func loopTone() []float32 {
	const tremolo = 3.0 // Hz amplitude wobble — gives the tone an easy-to-localize pulse
	n := sampleRate     // exactly 1 second
	pcm := make([]float32, n)
	for i := range pcm {
		t := float64(i) / sampleRate
		amp := 0.35 * (0.7 + 0.3*math.Sin(2*math.Pi*tremolo*t))
		pcm[i] = float32(amp * math.Sin(2*math.Pi*toneFreq*t))
	}
	return pcm
}

// --- fly camera -----------------------------------------------------------

// CamCtl is a fly camera plus the mouse-look input state.
type CamCtl struct {
	Yaw, Pitch float32 // radians; yaw 0 looks down -Z
	Pos        m.Vec3

	inited       bool
	looking      bool    // right mouse held
	lastX, lastY float64 // last cursor pos for look deltas
	haveLast     bool
}

const (
	moveSpeed = 6.0   // m/s
	lookSpeed = 0.005 // rad per pixel
)

func (ctl *CamCtl) forward() m.Vec3 {
	cp := float32(math.Cos(float64(ctl.Pitch)))
	sp := float32(math.Sin(float64(ctl.Pitch)))
	sy := float32(math.Sin(float64(ctl.Yaw)))
	cy := float32(math.Cos(float64(ctl.Yaw)))
	return m.Vec3{X: cp * sy, Y: sp, Z: -cp * cy}
}

func (ctl *CamCtl) apply(cam *render3d.Camera3D) {
	cam.Position = ctl.Pos
	cam.Target = ctl.Pos.Add(ctl.forward())
	cam.Up = m.Vec3{Y: 1}
}

func controlsSystem(c *app.Ctx) {
	cam := app.GetResource[render3d.Camera3D](c)
	ctl := app.GetResource[CamCtl](c)
	tm := app.GetResource[splititime.Time](c)
	if cam == nil || ctl == nil || tm == nil {
		return
	}
	if !ctl.inited {
		// Start standing back from the orbit at head height, looking at origin.
		ctl.Pos = m.Vec3{X: 0, Y: 1.6, Z: 14}
		dir := m.Vec3{Y: orbitHeight}.Sub(ctl.Pos).Normalize()
		ctl.Pitch = float32(math.Asin(float64(clamp(dir.Y, -1, 1))))
		ctl.Yaw = float32(math.Atan2(float64(dir.X), float64(-dir.Z)))
		ctl.inited = true
	}
	dt := float32(tm.Delta().Seconds())

	for _, ev := range app.ReadEvents[inputs.KeyEvent](c) {
		if ev.Key == inputs.KeyEscape && ev.Action == inputs.Press {
			c.App().Stop()
		}
	}
	handleButtons(c, ctl)
	handleLook(c, ctl)

	// Movement relative to the view, kept horizontal for W/S/A/D; Q/E are vertical.
	fwd := ctl.forward()
	flat := m.Vec3{X: fwd.X, Z: fwd.Z}.Normalize()
	right := flat.Cross(m.Vec3{Y: 1}).Normalize()
	var move m.Vec3
	if render3d.KeyDown(c, inputs.KeyW) {
		move = move.Add(flat)
	}
	if render3d.KeyDown(c, inputs.KeyS) {
		move = move.Sub(flat)
	}
	if render3d.KeyDown(c, inputs.KeyD) {
		move = move.Add(right)
	}
	if render3d.KeyDown(c, inputs.KeyA) {
		move = move.Sub(right)
	}
	if render3d.KeyDown(c, inputs.KeyE) {
		move = move.Add(m.Vec3{Y: 1})
	}
	if render3d.KeyDown(c, inputs.KeyQ) {
		move = move.Sub(m.Vec3{Y: 1})
	}
	if move != (m.Vec3{}) {
		ctl.Pos = ctl.Pos.Add(move.Normalize().Scale(moveSpeed * dt))
	}
	ctl.apply(cam)
}

func handleButtons(c *app.Ctx, ctl *CamCtl) {
	for _, ev := range app.ReadEvents[inputs.MouseButtonEvent](c) {
		if ev.Button == inputs.MouseButtonRight {
			ctl.looking = ev.Action == inputs.Press
			ctl.haveLast = false
		}
	}
}

func handleLook(c *app.Ctx, ctl *CamCtl) {
	for _, ev := range app.ReadEvents[inputs.MouseMoveEvent](c) {
		if !ctl.looking || !ctl.haveLast {
			ctl.lastX, ctl.lastY = ev.X, ev.Y
			ctl.haveLast = true
			continue
		}
		dx := float32(ev.X - ctl.lastX)
		dy := float32(ev.Y - ctl.lastY)
		ctl.lastX, ctl.lastY = ev.X, ev.Y
		ctl.Yaw += dx * lookSpeed
		ctl.Pitch -= dy * lookSpeed
		const lim = math.Pi/2 - 0.02
		ctl.Pitch = clamp(ctl.Pitch, -lim, lim)
	}
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
