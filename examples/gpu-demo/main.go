// Command gpu-demo is a minimal showcase of the GPU render backend
// (plugin/webgpu): a handful of textured, tinted quads bouncing inside a window,
// driven by the ordinary spliti app loop and ECS — proof that rendering off the
// terminal is just another plugin against the same engine.
//
// spliti keeps owning the loop; webgpu.Plugin only installs the window, the
// render/present systems, and a GLFW input poll. Quit with Escape or the window
// close button.
//
// Requires a GPU window, cgo, and a C toolchain:
//
//	CGO_ENABLED=1 go run ./examples/gpu-demo
package main

import (
	"image"
	"runtime"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/webgpu"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
)

// The GLFW window must live on the main OS thread. webgpu.Plugin.Build also
// locks it, but doing it here in init() documents the requirement for any game
// using this backend.
func init() { runtime.LockOSThread() }

// world dimensions (the visible rect the camera maps to the window).
const (
	worldW = 80.0
	worldH = 60.0
	ballSz = 5.0
)

// Velocity moves a Transform each Update tick, in world units per second.
type Velocity struct{ X, Y float32 }

func main() {
	a := app.New()
	a.AddPlugins(
		splititime.Plugin{TargetFrameRate: 60},
		webgpu.Plugin{
			Width: 800, Height: 600,
			Title:      "spliti — gpu demo",
			WorldW:     worldW,
			WorldH:     worldH,
			ClearColor: webgpu.Color{R: 0.05, G: 0.06, B: 0.10, A: 1},
		},
	)

	a.AddSystems(schedule.Startup, setup)
	a.AddSystems(schedule.Update, move)
	a.AddSystems(schedule.Update, quitOnEscape)

	a.Run()
}

// palette tints successive balls; the texture itself is white so the tint is the
// visible color.
var palette = []webgpu.Color{
	{R: 0.96, G: 0.26, B: 0.21, A: 1}, // red
	{R: 0.30, G: 0.69, B: 0.31, A: 1}, // green
	{R: 0.13, G: 0.59, B: 0.95, A: 1}, // blue
	{R: 1.00, G: 0.76, B: 0.03, A: 1}, // amber
	{R: 0.61, G: 0.15, B: 0.69, A: 1}, // purple
	{R: 0.00, G: 0.74, B: 0.83, A: 1}, // cyan
}

func setup(c *app.Ctx) {
	reg := app.GetResource[webgpu.TextureRegistry](c)
	if err := reg.Load("ball", circleTexture(64)); err != nil {
		panic(err)
	}

	const n = 6
	for i := 0; i < n; i++ {
		i := i
		app.Spawn4[webgpu.Transform, webgpu.Sprite, webgpu.Color, Velocity](
			c.Commands(),
			func(t *webgpu.Transform, s *webgpu.Sprite, col *webgpu.Color, v *Velocity) {
				// Deterministic spread across the world rect.
				t.X = float32(8 + (i*11)%int(worldW-ballSz))
				t.Y = float32(6 + (i*7)%int(worldH-ballSz))
				t.W, t.H = ballSz, ballSz
				s.Ref = "ball"
				*col = palette[i%len(palette)]
				v.X = float32(14 + 3*i)
				v.Y = float32(11 + 4*((i%3)+1))
				if i%2 == 0 {
					v.X = -v.X
				}
				if i%3 == 0 {
					v.Y = -v.Y
				}
			},
		)
	}
}

func move(c *app.Ctx) {
	t := app.GetResource[splititime.Time](c)
	dt := float32(t.Delta().Seconds())
	app.Query2[webgpu.Transform, Velocity](c, func(_ ecs.Entity, tr *webgpu.Transform, v *Velocity) {
		tr.X += v.X * dt
		tr.Y += v.Y * dt
		if tr.X < 0 {
			tr.X = 0
			v.X = -v.X
		}
		if tr.X+tr.W > worldW {
			tr.X = worldW - tr.W
			v.X = -v.X
		}
		if tr.Y < 0 {
			tr.Y = 0
			v.Y = -v.Y
		}
		if tr.Y+tr.H > worldH {
			tr.Y = worldH - tr.H
			v.Y = -v.Y
		}
	})
}

func quitOnEscape(c *app.Ctx) {
	for _, ev := range app.ReadEvents[webgpu.KeyEvent](c) {
		if ev.Key == glfw.KeyEscape && ev.Action == glfw.Press {
			c.App().Stop()
		}
	}
}

// circleTexture builds a size×size white RGBA disc on a transparent background,
// so tints show as solid colored balls with smooth-ish edges.
func circleTexture(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	cx, cy := r-0.5, r-0.5
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			if dx*dx+dy*dy <= r*r {
				i := (y*size + x) * 4
				img.Pix[i+0] = 255
				img.Pix[i+1] = 255
				img.Pix[i+2] = 255
				img.Pix[i+3] = 255
			}
		}
	}
	return img
}
