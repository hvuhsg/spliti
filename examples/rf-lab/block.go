package main

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// A block is a tall, opaque wall that occludes line-of-sight: a transmitter → point
// path that crosses its footprint reaches the far side only by diffracting over the
// wall's top edge. That detour's excess path grows with the wall's height above the
// antennas, so a tall wall casts a deep shadow while a short one is passed over
// freely — and the shadow is frequency-dependent, a short wavelength (high carrier)
// being extinguished where a long one still bends over and leaks in. So you can
// watch the shadow harden as you tune a transmitter up in frequency, see a link drop
// out when a wall is dragged between the pair, and kill a link entirely by walling a
// receiver in. It is a plain tagged box with a Transform3D, pickable and draggable
// like the device markers but carrying no radio state.
const (
	blockW = 8.0  // footprint width (X), meters
	blockH = 16.0 // height (Y), meters — tall enough to read as a wall
	blockD = 8.0  // footprint depth (Z), meters

	// shadowFloor is the residual amplitude transmitted into a fully developed
	// shadow, so a blocked link reads as deeply attenuated rather than exactly zero
	// (which would send the SNR to -inf and the ground shadow to pure black).
	shadowFloor = 0.04
	// shadowDecay sets how fast the diffracted field falls off with the excess
	// path length around the obstacle edge, measured in wavelengths. Larger values
	// give crisper, darker shadows.
	shadowDecay = 6.0
)

// blockTag marks an obstacle entity (the pickable box). There may be any number.
type blockTag struct{}

// spawnBlock spawns an obstacle box centered on the ground at pos. When sel is
// true the new block becomes the current selection (used for runtime adds).
func spawnBlock(cmd *app.Commands, pos m.Vec3, lab *Lab, sel bool) {
	t := render3d.NewTransform3D(pos)
	t.Scale = m.Vec3{X: blockW, Y: blockH, Z: blockD}
	cmd.Add(func(w *ecs.World) {
		mp := generic.NewMap5[render3d.Transform3D, render3d.GlobalTransform, render3d.MeshRenderer, render3d.MaterialRef, blockTag](w)
		e := mp.NewWith(&t, &render3d.GlobalTransform{Matrix: m.Identity4()},
			&render3d.MeshRenderer{Mesh: "block"}, &render3d.MaterialRef{Material: "block"}, &blockTag{})
		if sel {
			lab.Sel, lab.Ent = SelBlock, e
		}
	})
}

// blockBox is one wall's axis-aligned ground footprint (center + half-extents in XZ)
// plus the height of its top edge, derived from its transform so a resized wall
// occludes its true area and shadows by its true height.
type blockBox struct{ cx, cz, hx, hz, topY float64 }

// gatherBlocks collects every wall's footprint and top height for this frame's LoS
// tests. There are only a handful, so a per-frame slice is cheap.
func gatherBlocks(c *app.Ctx) []blockBox {
	var bs []blockBox
	app.Query2[render3d.Transform3D, blockTag](c,
		func(_ ecs.Entity, t *render3d.Transform3D, _ *blockTag) {
			bs = append(bs, blockBox{
				cx: float64(t.Translation.X), cz: float64(t.Translation.Z),
				hx: float64(t.Scale.X) / 2, hz: float64(t.Scale.Z) / 2,
				topY: float64(t.Translation.Y) + float64(t.Scale.Y)/2,
			})
		})
	return bs
}

// losAmp returns the line-of-sight field-amplitude transmission factor in
// [shadowFloor, 1] for a wave of the given frequency travelling the ground segment
// a→b: 1 when the path is clear, falling toward shadowFloor the deeper b sits in a
// block's diffraction shadow (and the shorter the wavelength). Multiple blocks
// compound multiplicatively. Used by the wave visualization and receiver sampling,
// which work in amplitude.
func losAmp(ax, az, bx, bz, freqHz float64, blocks []blockBox) float64 {
	if freqHz <= 0 || len(blocks) == 0 {
		return 1
	}
	lambda := sim.SpeedOfLight / freqHz
	a := 1.0
	for i := range blocks {
		a *= edgeDiffraction(ax, az, bx, bz, lambda, blocks[i])
		if a <= shadowFloor {
			return shadowFloor
		}
	}
	return a
}

// edgeDiffraction returns the amplitude reaching b past one wall. With a clear line
// of sight (the segment misses the footprint) it is 1. Where the footprint is
// crossed, the wave lies in the wall's geometric shadow and reaches b only by
// diffracting over the top edge — the antennas are too low to see past a tall,
// opaque wall any other way. That detour costs the excess path
//
//	δ = |a→top| + |top→b| − |a→b|
//
// measured in the vertical plane along the path, where the top edge sits clr =
// topY − markerHeight above the antenna-height line of sight. δ grows with the
// wall's height (a taller wall shadows more) and the diffracted amplitude falls as
// exp(−shadowDecay·δ/λ): a high carrier (small λ) is extinguished where a low one
// still bends over, which is why long waves visibly leak past a wall a short one
// cannot. A wall no taller than the antennas (clr ≤ 0) is passed over freely.
func edgeDiffraction(ax, az, bx, bz, lambda float64, b blockBox) float64 {
	if !segHitsBox(ax, az, bx, bz, b) {
		return 1
	}
	clr := b.topY - markerHeight // wall rise above the antenna-height line of sight
	if clr <= 0 {
		return 1 // shorter than the antennas: the wave clears the top with no detour
	}
	dx, dz := bx-ax, bz-az
	dAB := math.Hypot(dx, dz)
	if dAB < 1e-9 {
		return 1
	}
	// Split the horizontal path at the wall by projecting its center onto the segment,
	// so the detour climbs over the top at the crossing point.
	t := ((b.cx-ax)*dx + (b.cz-az)*dz) / (dAB * dAB)
	t = math.Max(0, math.Min(1, t))
	d1, d2 := t*dAB, (1-t)*dAB
	delta := math.Hypot(d1, clr) + math.Hypot(d2, clr) - dAB
	if delta < 0 {
		delta = 0
	}
	a := math.Exp(-shadowDecay * delta / lambda)
	if a < shadowFloor {
		a = shadowFloor
	}
	return a
}

// segHitsBox reports whether the 2D segment a→b crosses box b's footprint, via
// the slab method over the parameter t ∈ [0,1].
func segHitsBox(ax, az, bx, bz float64, b blockBox) bool {
	tmin, tmax := 0.0, 1.0
	if !slab(ax, bx-ax, b.cx-b.hx, b.cx+b.hx, &tmin, &tmax) {
		return false
	}
	if !slab(az, bz-az, b.cz-b.hz, b.cz+b.hz, &tmin, &tmax) {
		return false
	}
	return tmin <= tmax
}

// slab clips the segment parameter range [tmin,tmax] against one axis' [lo,hi]
// span (origin o, direction d), returning false when the segment misses it.
func slab(o, d, lo, hi float64, tmin, tmax *float64) bool {
	if math.Abs(d) < 1e-12 {
		return o >= lo && o <= hi // parallel: in range only if it starts inside
	}
	t1, t2 := (lo-o)/d, (hi-o)/d
	if t1 > t2 {
		t1, t2 = t2, t1
	}
	if t1 > *tmin {
		*tmin = t1
	}
	if t2 < *tmax {
		*tmax = t2
	}
	return *tmin <= *tmax
}
