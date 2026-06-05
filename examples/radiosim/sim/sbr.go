package sim

import (
	"math"
	"math/cmplx"
	"runtime"
	"strconv"
	"sync"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// SBREngine is a shooting-bouncing-rays propagation engine: it launches a bundle
// of rays uniformly from the transmitter, marches each through the scene
// reflecting off faces (reusing the same Fresnel Interaction physics as the image
// method), and collects the rays that pass within a reception sphere of the
// receiver. Unlike the image method it scales ~linearly in rays × bounces and
// parallelizes across goroutines, so it is the real-time engine for detailed
// scenes and the natural target for a future GPU port. It is approximate: ray
// density and the reception-sphere radius trade speed against accuracy
// (Config.NumRays is the knob).
//
// SBR here models the direct path and specular reflections. Transmission and
// diffraction remain image-method features; an SBR build would add them as ray
// children, at more cost.
type SBREngine struct{}

// Paths launches rays from tx and returns the direct and reflected paths captured
// at rx. Rays are split across worker goroutines; results are deterministic
// (directions come from a Fibonacci sphere, captures are merged by closest
// approach) and independent of the worker count.
func (SBREngine) Paths(tx Tx, rx Rx, sc *Scene, cfg Config) []Path {
	return sbrPaths(tx, rx.Pos, sc, cfg, newInteraction(sc, cfg))
}

// Coverage evaluates received power at every grid cell by launching an
// independent ray bundle per cell, parallelized across cells.
func (SBREngine) Coverage(tx Tx, grid Grid, sc *Scene, cfg Config) []float64 {
	inter := newInteraction(sc, cfg)
	field := make([]float64, grid.Len())
	var wg sync.WaitGroup
	cells := make(chan int, grid.Len())
	for w := 0; w < workers(cfg); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range cells {
				ix, iz := idx%grid.NX, idx/grid.NX
				p := grid.Cell(ix, iz)
				if p.Sub(tx.Pos).LengthSq() < 1e-4 {
					continue
				}
				pw, _ := Received(sbrPaths(tx, p, sc, cfg, inter))
				field[idx] = pw
			}
		}()
	}
	for idx := 0; idx < grid.Len(); idx++ {
		cells <- idx
	}
	close(cells)
	wg.Wait()
	return field
}

// capture is the best (closest-approach) ray that reached the receiver for a given
// bounce-face sequence.
type capture struct {
	L      float64
	gain   complex128
	dist   float32
	points []m.Vec3
}

// sbrPaths is the ray-launch core: it shoots Config.NumRays rays from tx, captures
// those passing within the reception sphere of rxPos, dedups them to the closest
// ray per bounce sequence, and builds one Path per sequence.
func sbrPaths(tx Tx, rxPos m.Vec3, sc *Scene, cfg Config, inter Interaction) []Path {
	lambda := tx.Wavelength()
	if lambda <= 0 {
		return nil
	}
	k := 2 * math.Pi / lambda
	sqrtPt := math.Sqrt(math.Max(tx.PowerW, 0))
	n := cfg.NumRays
	if n <= 0 {
		n = 20000
	}
	nw := workers(cfg)

	// Each worker captures into its own map, merged by closest approach.
	partials := make([]map[string]*capture, nw)
	var wg sync.WaitGroup
	for w := 0; w < nw; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			local := map[string]*capture{}
			for i := w; i < n; i += nw {
				traceRay(tx.Pos, fibonacciDir(i, n), rxPos, sc, cfg, inter, tx.FreqHz, n, local)
			}
			partials[w] = local
		}(w)
	}
	wg.Wait()

	merged := map[string]*capture{}
	for _, local := range partials {
		for seq, c := range local {
			if cur, ok := merged[seq]; !ok || c.dist < cur.dist {
				merged[seq] = c
			}
		}
	}

	paths := make([]Path, 0, len(merged))
	for _, c := range merged {
		if c.L < 1e-6 {
			continue
		}
		mag := sqrtPt * lambda / (4 * math.Pi * c.L)
		amp := complex(mag, 0) * c.gain * cmplx.Exp(complex(0, -k*c.L))
		order := len(c.points) - 2 // points = tx, bounces..., approach
		paths = append(paths, Path{
			Length: float32(c.L),
			Delay:  float32(c.L / SpeedOfLight),
			Amp:    amp,
			Points: c.points,
			Order:  order,
		})
	}
	applyAtmosphere(paths, sc.Weather, tx.FreqHz)
	return paths
}

// traceRay marches one ray, recording a capture for each segment whose closest
// approach to rxPos falls inside the reception sphere, keyed by the bounce
// sequence so each physical path is captured once (the closest ray wins).
func traceRay(origin, dir, rxPos m.Vec3, sc *Scene, cfg Config, inter Interaction, fHz float64, nRays int, out map[string]*capture) {
	const farRange = 1e5
	p := origin
	d := dir.Normalize()
	gain := complex(1, 0)
	L := 0.0
	seq := ""
	pts := []m.Vec3{origin}

	for bounce := 0; ; bounce++ {
		tHit, fi, hit := nearestFaceHit(p, d, sc.Faces)
		segLen := float32(farRange)
		if hit {
			segLen = tHit
		}
		// Closest approach to the receiver along this segment.
		tStar := float32(rxPos.Sub(p).Dot(d))
		if tStar > 1e-6 && tStar < segLen-1e-6 {
			cp := p.Add(d.Scale(tStar))
			dist := cp.Sub(rxPos).Length()
			lc := L + float64(tStar)
			if dist < receptionRadius(lc, nRays, cfg.RxRadiusM) {
				if cur, ok := out[seq]; !ok || dist < cur.dist {
					capPts := append(append([]m.Vec3{}, pts...), cp)
					out[seq] = &capture{L: lc, gain: gain, dist: dist, points: capPts}
				}
			}
		}

		if !hit || bounce >= cfg.MaxOrder {
			return
		}
		hitPt := p.Add(d.Scale(tHit))
		g, refl := inter.OnReflect(fi, hitPt, d, fHz, TE)
		gain *= g
		if cmplx.Abs(gain) < 1e-7 {
			return
		}
		L += float64(tHit)
		d = refl.Normalize()
		p = hitPt.Add(d.Scale(1e-4)) // nudge off the surface
		pts = append(pts, hitPt)
		seq += strconv.Itoa(fi) + ","
	}
}

// receptionRadius returns the reception-sphere radius at path length L. With the
// adaptive default it is the ray-tube radius 2L/√N (so on average one ray of each
// bundle reaches the sphere); a positive Config.RxRadiusM overrides it.
func receptionRadius(L float64, nRays int, fixed float64) float32 {
	if fixed > 0 {
		return float32(fixed)
	}
	r := 2 * L / math.Sqrt(float64(nRays))
	if r < 1e-3 {
		r = 1e-3
	}
	return float32(r)
}

// nearestFaceHit returns the nearest face the ray origin+t·dir (t>0) strikes.
func nearestFaceHit(origin, dir m.Vec3, faces []Face) (tHit float32, fi int, ok bool) {
	best := float32(math.MaxFloat32)
	for i := range faces {
		if t, _, hit := rayFace(origin, dir, faces[i]); hit && t > 1e-4 && t < best {
			best, fi, ok = t, i, true
		}
	}
	return best, fi, ok
}

// fibonacciDir returns the i-th of n directions on a Fibonacci sphere — a
// near-uniform, deterministic spherical point set (no RNG, so SBR is
// reproducible).
func fibonacciDir(i, n int) m.Vec3 {
	ga := math.Pi * (3 - math.Sqrt(5)) // golden angle
	y := 1 - 2*(float64(i)+0.5)/float64(n)
	r := math.Sqrt(math.Max(0, 1-y*y))
	theta := ga * float64(i)
	return m.Vec3{X: float32(math.Cos(theta) * r), Y: float32(y), Z: float32(math.Sin(theta) * r)}
}

// workers returns the goroutine count for a run: Config.Workers if set, else
// the available CPUs (less two, minimum one).
func workers(cfg Config) int {
	if cfg.Workers > 0 {
		return cfg.Workers
	}
	n := runtime.NumCPU() - 2
	if n < 1 {
		n = 1
	}
	return n
}
