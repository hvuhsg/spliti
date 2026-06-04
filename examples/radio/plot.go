package main

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
	"github.com/mlange-42/arche/ecs"
)

// Dot tags one point of a plotted curve. A scene spawns a pool of dots for each
// series at OnEnter and repositions them every frame from the DSP math, instead
// of regenerating a curve texture per frame (which would re-upload to the GPU).
type Dot struct {
	Series int // which curve this dot belongs to (scene-defined ids)
	Index  int // sample index within the series, 0..Count-1
	Count  int // number of samples in the series
}

// viewport is a world-space rectangle a plot is drawn into (top-left origin,
// Y down — the engine convention).
type viewport struct{ X, Y, W, H float32 }

// dataRect is the data-space window mapped onto a viewport. Y is in math
// convention (up = positive); mapPoint flips it to the screen's Y-down.
type dataRect struct {
	X0, X1 float64 // horizontal data range (e.g. time)
	Y0, Y1 float64 // vertical data range (e.g. amplitude), Y1 at the top
}

// mapPoint maps a data-space point to world coordinates inside vp.
func (vp viewport) mapPoint(d dataRect, dx, dy float64) (float32, float32) {
	ux := (dx - d.X0) / (d.X1 - d.X0)
	uy := (dy - d.Y0) / (d.Y1 - d.Y0)
	wx := vp.X + float32(ux)*vp.W
	wy := vp.Y + vp.H - float32(uy)*vp.H // data-up -> screen-down
	return wx, wy
}

// placeSeries positions every dot of `series` at the world points produced by
// fn(index) -> (worldX, worldY). The point is the dot's centre. Dots whose index
// is out of range are parked off-screen.
func placeSeries(c *app.Ctx, series int, fn func(index int) (float32, float32)) {
	app.Query2[webgpu.Transform, Dot](c, func(_ ecs.Entity, t *webgpu.Transform, d *Dot) {
		if d.Series != series {
			return
		}
		x, y := fn(d.Index)
		t.X = x - t.W/2
		t.Y = y - t.H/2
	})
}

// recolorSeries sets the colour of every dot of `series` from fn(index).
func recolorSeries(c *app.Ctx, series int, fn func(index int) webgpu.Color) {
	app.Query2[webgpu.Color, Dot](c, func(_ ecs.Entity, col *webgpu.Color, d *Dot) {
		if d.Series != series {
			return
		}
		*col = fn(d.Index)
	})
}

// spawnSeries spawns a pool of `count` dots for one series.
func spawnSeries(cmds *app.Commands, series, count int, size float32, col webgpu.Color) {
	for k := 0; k < count; k++ {
		spawnDot(cmds, series, k, count, size, col)
	}
}

// spawnMarkers spawns `count` repositionable entities for one series using an
// arbitrary texture and a rectangular size — for highlight boxes, playheads, and
// other movable overlays that aren't round dots. placeSeries moves them by centre.
func spawnMarkers(cmds *app.Commands, series, count int, ref string, w, h float32, col webgpu.Color, z int) {
	for k := 0; k < count; k++ {
		k := k
		spawn5[webgpu.Transform, webgpu.Sprite, webgpu.Color, webgpu.Layer, Dot](cmds,
			func(t *webgpu.Transform, s *webgpu.Sprite, c *webgpu.Color, l *webgpu.Layer, d *Dot) {
				t.W, t.H = w, h
				t.X, t.Y = -50, -50
				s.Ref = ref
				*c = col
				l.Z = z
				d.Series, d.Index, d.Count = series, k, count
			})
	}
}
