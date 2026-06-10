//go:build !js

package main

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
)

// A scenario is a one-click lab layout. The features it exercises all exist already
// (multiple transmitters, walls, the channel toggles); the presets just arrange them
// so the effect is visible immediately instead of having to be discovered by hand.
type scenario int

const (
	scenClean scenario = iota
	scenInterference
	scenWall
	scenFading
)

type scenarioDef struct {
	name string
	desc string
}

// scenarioDefs is the ordered list shown as buttons; the index is the scenario value.
var scenarioDefs = []scenarioDef{
	{"Clean link", "One transmitter, one receiver, no obstacles or reflections — the bare link."},
	{"Co-channel", "Two transmitters on the same carrier reach one receiver; their symbols blur the constellation."},
	{"Hidden node", "A tall wall between the pair blocks line of sight; turn the carrier down or up to see the shadow deepen."},
	{"Multipath fading", "Ground reflection on: the two-ray sum lays fading nulls along the link as you move the receiver."},
}

// applyScenario clears the current devices and rebuilds the chosen layout, also
// setting the channel toggles the scenario means to demonstrate. The despawns are
// queued before the spawns on the same command buffer, so they apply in order.
func applyScenario(c *app.Ctx, lab *Lab, ch *Channel, s scenario) {
	clearScenario(c, lab)
	cmd := c.Commands()
	switch s {
	case scenClean:
		spawnTx(cmd, m.Vec3{X: -30, Y: markerHeight}, lab, false)
		spawnRx(cmd, m.Vec3{X: 30, Y: markerHeight}, lab, false)
		ch.Multipath, ch.Ground = false, false

	case scenInterference:
		// Two transmitters, both on the default 24 MHz carrier, illuminating one
		// receiver — co-channel interference the front-end filter cannot separate.
		spawnTx(cmd, m.Vec3{X: -30, Y: markerHeight, Z: -16}, lab, false)
		spawnTx(cmd, m.Vec3{X: -30, Y: markerHeight, Z: 16}, lab, false)
		spawnRx(cmd, m.Vec3{X: 36, Y: markerHeight}, lab, false)
		ch.Multipath, ch.Ground = false, false

	case scenWall:
		spawnTx(cmd, m.Vec3{X: -40, Y: markerHeight}, lab, false)
		spawnRx(cmd, m.Vec3{X: 40, Y: markerHeight}, lab, false)
		spawnBlock(cmd, m.Vec3{X: 0, Y: blockH / 2}, lab, false)
		// Wall multipath on so the reflected paths that sneak around the obstacle are
		// visible against the shadowed direct path.
		ch.Multipath, ch.Ground, ch.MaxOrder = true, false, 2

	case scenFading:
		spawnTx(cmd, m.Vec3{X: -35, Y: markerHeight}, lab, false)
		spawnRx(cmd, m.Vec3{X: 35, Y: markerHeight}, lab, false)
		ch.Multipath, ch.Ground, ch.GroundRefl = false, true, 0.9
	}
}

// clearScenario despawns every transmitter, receiver, and wall (and the masts
// parented to the markers), leaving the ground, field grid, and lighting. Only the
// masts carry a Parent in this scene, so despawning all Parent-tagged entities
// removes them without touching anything else.
func clearScenario(c *app.Ctx, lab *Lab) {
	cmd := c.Commands()
	app.Query1[txTag](c, func(e ecs.Entity, _ *txTag) { cmd.Despawn(e) })
	app.Query1[rxTag](c, func(e ecs.Entity, _ *rxTag) { cmd.Despawn(e) })
	app.Query1[blockTag](c, func(e ecs.Entity, _ *blockTag) { cmd.Despawn(e) })
	app.Query1[render3d.Parent](c, func(e ecs.Entity, _ *render3d.Parent) { cmd.Despawn(e) })
	lab.Sel, lab.Ent = SelNone, ecs.Entity{}
}
