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
	"flag"
	"fmt"
	"image"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	gotime "time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/hvuhsg/spliti/app"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/webgpu"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
)

// Profiling flags. -trace writes a runtime/trace file (open with `go tool trace`)
// — the best tool for stutter, since it shows GC stop-the-world, goroutine
// scheduling, and syscall blocking on a timeline. -cpuprofile writes a pprof CPU
// profile (`go tool pprof`). Both are off by default.
var (
	traceFile = flag.String("trace", "", "write runtime/trace to this file")
	cpuFile   = flag.String("cpuprofile", "", "write a pprof CPU profile to this file")
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

// Body is a ball's physics state. Velocity is in world units per second; Prev
// and Cur are the ball's center-of-motion (top-left) position at the start and
// end of the most recent fixed step. The render pass interpolates between them
// (see interpolate) so motion stays smooth when the display refresh and the
// 64 Hz fixed step don't divide evenly. Transform.X/Y is the interpolated
// draw position and is written only by interpolate, never by the sim.
type Body struct {
	VX, VY       float32
	PrevX, PrevY float32
	CurX, CurY   float32
}

func main() {
	flag.Parse()

	// Profiling start/stop. Stops run on a clean exit (Escape / window close),
	// which ends a.Run() and returns here.
	if *cpuFile != "" {
		f, err := os.Create(*cpuFile)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			panic(err)
		}
		defer pprof.StopCPUProfile()
	}
	if *traceFile != "" {
		f, err := os.Create(*traceFile)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := trace.Start(f); err != nil {
			panic(err)
		}
		defer trace.Stop()
	}

	a := app.New()
	a.AddPlugins(
		// No frame cap: VSync (FIFO) below paces the loop to the display refresh.
		// A TargetFrameRate sleep here would fight vsync — e.g. a 60 cap on a
		// 120 Hz panel lands frames at ~2.1 refresh intervals, causing judder.
		splititime.Plugin{TargetFrameRate: 0},
		webgpu.Plugin{
			Width: 800, Height: 600,
			Title:      "spliti — gpu demo",
			WorldW:     worldW,
			WorldH:     worldH,
			ClearColor: webgpu.Color{R: 0.05, G: 0.06, B: 0.10, A: 1},
			Smooth:     true, // linear filtering -> anti-aliased sprite edges
			VSync:      true, // FIFO present: lock to display refresh, no tearing/beat
		},
	)

	a.AddSystems(schedule.Startup, setup)
	a.AddSystems(schedule.FixedUpdate, move) // physics on the fixed step, like Unity's FixedUpdate
	// interpolate runs every rendered frame, before the sprite pass, writing the
	// lerp between the last two fixed states into each Transform — the render half
	// of the fixed-timestep pattern.
	webgpu.AddPreRender(a, interpolate)
	a.AddSystems(schedule.Update, quitOnEscape)
	a.AddSystems(schedule.Last, frameStats())

	a.Run()
}

// frameStats returns a system that accumulates per-frame deltas (from the time
// plugin) and prints a jitter summary once a second: average/min/max frame time,
// the resulting FPS, and how many frames blew past 1.5x the target — the spikes
// you feel as stutter. Reporting min/max/spikes (not just average FPS) is the
// point: 60 FPS on average with a handful of 30ms frames still looks janky.
func frameStats() app.SystemFunc {
	const target = gotime.Second / 60
	var (
		count, spikes int
		accum, since  gotime.Duration
		lo, hi        gotime.Duration
	)
	lo = gotime.Hour
	return func(c *app.Ctx) {
		t := app.GetResource[splititime.Time](c)
		if t == nil {
			return
		}
		d := t.Delta()
		count++
		accum += d
		since += d
		if d < lo {
			lo = d
		}
		if d > hi {
			hi = d
		}
		if d > target*3/2 {
			spikes++
		}
		if since >= gotime.Second {
			avg := accum / gotime.Duration(count)
			fmt.Printf("frame: avg=%.2fms (%.0f fps)  min=%.2fms  max=%.2fms  spikes>%.0fms=%d/%d\n",
				ms(avg), 1/avg.Seconds(), ms(lo), ms(hi), ms(target*3/2), spikes, count)
			count, spikes, accum, since = 0, 0, 0, 0
			lo, hi = gotime.Hour, 0
		}
	}
}

func ms(d gotime.Duration) float64 { return float64(d) / float64(gotime.Millisecond) }

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
	if err := reg.Load("ball", circleTexture(128)); err != nil {
		panic(err)
	}

	const n = 6
	for i := 0; i < n; i++ {
		i := i
		app.Spawn4[webgpu.Transform, webgpu.Sprite, webgpu.Color, Body](
			c.Commands(),
			func(t *webgpu.Transform, s *webgpu.Sprite, col *webgpu.Color, b *Body) {
				// Deterministic spread across the world rect.
				x := float32(8 + (i*11)%int(worldW-ballSz))
				y := float32(6 + (i*7)%int(worldH-ballSz))
				t.X, t.Y = x, y
				t.W, t.H = ballSz, ballSz
				s.Ref = "ball"
				*col = palette[i%len(palette)]
				b.PrevX, b.PrevY = x, y
				b.CurX, b.CurY = x, y
				b.VX = float32(14 + 3*i)
				b.VY = float32(11 + 4*((i%3)+1))
				if i%2 == 0 {
					b.VX = -b.VX
				}
				if i%3 == 0 {
					b.VY = -b.VY
				}
			},
		)
	}
}

// move advances each ball's fixed-step position by its velocity, bouncing off
// the world edges. It runs in FixedUpdate, so it steps by the constant fixed
// timestep (not the variable render delta) — motion stays uniform regardless of
// frame-rate jitter. It writes only the Body's Cur/Prev, never the Transform;
// interpolate turns those into the rendered position.
func move(c *app.Ctx) {
	t := app.GetResource[splititime.Time](c)
	dt := float32(t.FixedDelta().Seconds())
	app.Query1[Body](c, func(_ ecs.Entity, b *Body) {
		b.PrevX, b.PrevY = b.CurX, b.CurY
		b.CurX += b.VX * dt
		b.CurY += b.VY * dt
		if b.CurX < 0 {
			b.CurX = 0
			b.VX = -b.VX
		}
		if b.CurX+ballSz > worldW {
			b.CurX = worldW - ballSz
			b.VX = -b.VX
		}
		if b.CurY < 0 {
			b.CurY = 0
			b.VY = -b.VY
		}
		if b.CurY+ballSz > worldH {
			b.CurY = worldH - ballSz
			b.VY = -b.VY
		}
	})
}

// interpolate writes lerp(Prev, Cur, alpha) into each Transform every rendered
// frame, where alpha is the fraction of a fixed step elapsed since the last
// FixedUpdate. This is the render half of the fixed-timestep pattern: without it,
// a 64 Hz sim shows visible stepping on a faster display; with it, the balls
// glide between fixed states.
func interpolate(c *app.Ctx) {
	t := app.GetResource[splititime.Time](c)
	if t == nil {
		return
	}
	alpha := float32(t.Alpha())
	app.Query2[webgpu.Transform, Body](c, func(_ ecs.Entity, tr *webgpu.Transform, b *Body) {
		tr.X = b.PrevX + (b.CurX-b.PrevX)*alpha
		tr.Y = b.PrevY + (b.CurY-b.PrevY)*alpha
	})
}

func quitOnEscape(c *app.Ctx) {
	for _, ev := range app.ReadEvents[webgpu.KeyEvent](c) {
		if ev.Key == glfw.KeyEscape && ev.Action == glfw.Press {
			c.App().Stop()
		}
	}
}

// circleTexture builds a size×size white RGBA disc on a transparent background.
// The edge is anti-aliased: each texel's alpha is the fraction of an ss×ss
// subsample grid that lands inside the disc, producing a smooth 0..255 coverage
// gradient instead of a hard cutoff. With linear sampling this is what gives the
// balls clean, non-jagged edges — the standard way 2D sprite engines anti-alias.
func circleTexture(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	cx, cy := r-0.5, r-0.5
	const ss = 4 // subsamples per axis (16 coverage samples per texel)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			inside := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss - 0.5
					py := float64(y) + (float64(sy)+0.5)/ss - 0.5
					dx, dy := px-cx, py-cy
					if dx*dx+dy*dy <= r*r {
						inside++
					}
				}
			}
			i := (y*size + x) * 4
			img.Pix[i+0] = 255
			img.Pix[i+1] = 255
			img.Pix[i+2] = 255
			img.Pix[i+3] = uint8(255 * inside / (ss * ss))
		}
	}
	return img
}
