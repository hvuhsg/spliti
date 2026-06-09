package main

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
)

// rayHeight lifts the drawn rays just above the antenna heads so the polylines
// read above the ground field instead of z-fighting with it.
const rayHeight = markerHeight + 0.4

// rayColors tints a path by its bounce order: the direct path is white, and each
// further bounce shifts toward a dimmer, warmer hue so you can read how many times
// a reflected ray has bounced.
var rayColors = [maxRefl + 1]m.Vec4{
	{X: 1.0, Y: 1.0, Z: 1.0, W: 0.9}, // direct
	{X: 1.0, Y: 0.78, Z: 0.30, W: 0.85},
	{X: 1.0, Y: 0.50, Z: 0.20, W: 0.75},
	{X: 0.95, Y: 0.32, Z: 0.20, W: 0.65},
}

// raysSystem draws the propagation rays for the focused transmitter → receiver
// pair: the direct line plus, when multipath is on, every specular reflection off
// the wall blocks, with a small marker at each bounce. It only enqueues gizmo
// segments (drawn by render3d's auto-registered overlay), so it makes no lasting
// scene changes.
func raysSystem(c *app.Ctx) {
	if ui := app.GetResource[UI](c); ui != nil && ui.Mode == ModeGraph {
		return // the node editor owns the screen in graph mode
	}
	lab := app.GetResource[Lab](c)
	ch := app.GetResource[Channel](c)
	if lab == nil {
		return
	}
	// Boresight arrows are an always-on aim aid for directional antennas, independent
	// of multipath.
	drawBoresights(c)
	// Rays are a multipath visualization aid; with multipath off the lab looks
	// exactly as it always has (no overlay).
	if ch == nil || !ch.Multipath {
		return
	}
	txd, txPos, okt := focusTx(c, lab)
	_, rxPos, okr := focusRx(c, lab)
	if !okt || !okr || txd == nil {
		return
	}
	// A transmitter with a severed chain radiates nothing, so it casts no rays.
	if !txRadiates(txd.Graph) {
		return
	}

	// The direct ray, plus every reflected path off the walls.
	tx := m.Vec3{X: txPos.X, Y: rayHeight, Z: txPos.Z}
	rx := m.Vec3{X: rxPos.X, Y: rayHeight, Z: rxPos.Z}
	render3d.Line(c, tx, rx, rayColors[0])

	blocks := gatherBlocks(c)
	if len(blocks) == 0 {
		return
	}
	nodes := buildImages(p2{x: float64(txPos.X), z: float64(txPos.Z)}, blockFaces(blocks), ch.MaxOrder)
	paths := reflectionRays(
		p2{x: float64(txPos.X), z: float64(txPos.Z)},
		p2{x: float64(rxPos.X), z: float64(rxPos.Z)},
		rayHeight, txd.FreqHz, blocks, nodes, ch.Reflectivity, ch.MaxOrder)
	for _, pth := range paths {
		col := rayColors[len(rayColors)-1]
		if pth.order < len(rayColors) {
			col = rayColors[pth.order]
		}
		render3d.Polyline(c, pth.pts, col)
		// Mark every bounce point (the interior vertices of the polyline).
		for i := 1; i < len(pth.pts)-1; i++ {
			render3d.Point(c, pth.pts[i], col)
		}
	}
}

// boresightLen is how far the aim arrow reaches from a directional antenna's head.
const boresightLen = 14.0

// drawBoresights draws a short arrow along the boresight of every directional
// transmitter (warm) and receiver (cool), so the heading a device is aimed at is
// visible in the scene rather than only a slider value.
func drawBoresights(c *app.Ctx) {
	txCol := m.Vec4{X: 1, Y: 0.85, Z: 0.4, W: 0.9}
	rxCol := m.Vec4{X: 0.4, Y: 0.9, Z: 1, W: 0.9}
	app.Query2[render3d.Transform3D, TxDevice](c, func(_ ecs.Entity, t *render3d.Transform3D, d *TxDevice) {
		if d.Directional {
			drawBoresight(c, t.Translation, d.Azimuth, txCol)
		}
	})
	app.Query2[render3d.Transform3D, RxDevice](c, func(_ ecs.Entity, t *render3d.Transform3D, d *RxDevice) {
		if d.Directional {
			drawBoresight(c, t.Translation, d.Azimuth, rxCol)
		}
	})
}

// drawBoresight draws one aim arrow from pos along azimuth (atan2(dz,dx)), lifted to
// ray height and capped with a point so the direction reads at a glance.
func drawBoresight(c *app.Ctx, pos m.Vec3, az float64, col m.Vec4) {
	from := m.Vec3{X: pos.X, Y: rayHeight, Z: pos.Z}
	to := m.Vec3{
		X: pos.X + float32(math.Cos(az)*boresightLen),
		Y: rayHeight,
		Z: pos.Z + float32(math.Sin(az)*boresightLen),
	}
	render3d.Line(c, from, to, col)
	render3d.Point(c, to, col)
}
