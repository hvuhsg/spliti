package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/screenshot"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// captureEnv names the environment variable that turns rf-lab into a
// self-driving screenshot session: set it to an output directory and the lab
// steps through each panel/UI state, writes a PNG for each, and quits. This is
// how the project's UI screenshots are produced without a human at the controls.
const captureEnv = "RF_LAB_SHOTS"

// shot is one scripted screenshot: a label (used as the file name), the dwell in
// frames to let the simulation settle before capturing, and a setup func that
// puts the lab into the state to photograph.
type shot struct {
	name  string
	dwell int
	setup func(c *app.Ctx, p *capturePlan)
}

// capturePlan is the screenshot driver's state: where to write, which shot we're
// on, and the resolved transmitter/receiver entities the shots manipulate.
type capturePlan struct {
	dir          string
	shots        []shot
	idx          int
	frame        int
	txEnt, rxEnt ecs.Entity
	shot         bool // RequestScreenshot already issued for the current shot
}

// captureEnabled reports the output directory and whether capture mode is on.
func captureEnabled() (string, bool) {
	dir := os.Getenv(captureEnv)
	return dir, dir != ""
}

func newCapturePlan(dir string) *capturePlan {
	return &capturePlan{dir: dir, shots: shotList()}
}

// shotList is the storyboard: each entry photographs one distinct piece of the
// rf-lab UI. Dwell times let the wave clock fill the scope and constellation
// ring buffers (and let the receiver decode a few characters) before the frame
// is grabbed.
func shotList() []shot {
	return []shot{
		{
			name: "01-explore-readout-scope", dwell: 90,
			setup: func(c *app.Ctx, p *capturePlan) {
				// Nothing selected: only the always-on link readout (top-left) and the
				// RX oscilloscope (bottom-left) are shown.
				deselect(c)
				p.setPlaying(c, false)
			},
		},
		{
			name: "02-tx-config-constellation", dwell: 260,
			setup: func(c *app.Ctx, p *capturePlan) {
				// Transmitter selected and sending: the TRANSMITTER config drawer (right)
				// plus the TX ideal-constellation panel (bottom) with the live symbol lit.
				selectTx(c, p.txEnt)
				p.setPlaying(c, true)
			},
		},
		{
			name: "03-rx-config-decode", dwell: 320,
			setup: func(c *app.Ctx, p *capturePlan) {
				// Receiver selected while the transmitter keeps sending: the RECEIVER
				// config drawer (right) and the RX received-scatter + decoded-text panel.
				selectRx(c, p.rxEnt)
				p.setPlaying(c, true)
			},
		},
		{
			name: "04-graph-tx-chain", dwell: 60,
			setup: func(c *app.Ctx, p *capturePlan) {
				// The node-graph editor for the transmitter's signal chain
				// (Text -> Constellation -> Transmitter).
				openGraph(c, SelTx, p.txEnt)
			},
		},
		{
			name: "05-graph-rx-chain", dwell: 120,
			setup: func(c *app.Ctx, p *capturePlan) {
				// The node-graph editor for the receiver's decode chain
				// (Receiver -> Constellation -> Text), decoding live.
				openGraph(c, SelRx, p.rxEnt)
			},
		},
	}
}

// captureSystem drives the storyboard one shot at a time. Each shot: run its
// setup on the first frame, dwell, request a screenshot, then advance. After the
// last shot it stops the app.
func captureSystem(c *app.Ctx) {
	p := app.GetResource[capturePlan](c)
	if p == nil || p.idx >= len(p.shots) {
		return
	}
	// Resolve the default transmitter/receiver entities (spawned at startup).
	if p.txEnt.IsZero() || p.rxEnt.IsZero() {
		p.resolveEntities(c)
		if p.txEnt.IsZero() || p.rxEnt.IsZero() {
			return // scene not spawned yet
		}
	}

	s := &p.shots[p.idx]
	if p.frame == 0 {
		s.setup(c, p)
	}
	p.frame++

	switch {
	case p.frame < s.dwell:
		// settling
	case !p.shot:
		path := filepath.Join(p.dir, s.name+".png")
		if screenshot.Save(c, path) {
			fmt.Fprintln(os.Stderr, "rf-lab capture: wrote", path)
		} else {
			fmt.Fprintln(os.Stderr, "rf-lab capture: renderer not ready for", path)
		}
		p.shot = true
	default:
		// One extra frame so the present that takes the screenshot has completed,
		// then move to the next shot.
		p.idx++
		p.frame, p.shot = 0, false
		if p.idx >= len(p.shots) {
			fmt.Fprintln(os.Stderr, "rf-lab capture: done")
			c.App().Stop()
		}
	}
}

// resolveEntities finds the first transmitter and receiver markers in the scene.
func (p *capturePlan) resolveEntities(c *app.Ctx) {
	app.Query1[txTag](c, func(e ecs.Entity, _ *txTag) {
		if p.txEnt.IsZero() {
			p.txEnt = e
		}
	})
	app.Query1[rxTag](c, func(e ecs.Entity, _ *rxTag) {
		if p.rxEnt.IsZero() {
			p.rxEnt = e
		}
	})
}

// setPlaying starts or stops every transmitter's playback, recompiling the chain
// when starting so the symbol stream is ready.
func (p *capturePlan) setPlaying(c *app.Ctx, on bool) {
	mp := generic.NewMap[TxDevice](c.World())
	app.Query1[txTag](c, func(e ecs.Entity, _ *txTag) {
		if !mp.Has(e) {
			return
		}
		d := mp.Get(e)
		if d.Play == nil {
			return
		}
		if on && !d.Play.Playing {
			d.Play.recompile(d.Graph)
		}
		d.Play.Playing = on
	})
}

// deselect clears the selection and returns to explore mode.
func deselect(c *app.Ctx) {
	if lab := app.GetResource[Lab](c); lab != nil {
		lab.Sel, lab.Ent = SelNone, ecs.Entity{}
	}
	if ui := app.GetResource[UI](c); ui != nil {
		ui.Mode = ModeExplore
	}
}

func selectTx(c *app.Ctx, e ecs.Entity) { selectMarker(c, SelTx, e) }
func selectRx(c *app.Ctx, e ecs.Entity) { selectMarker(c, SelRx, e) }

// selectMarker selects entity e as kind sel, in explore mode.
func selectMarker(c *app.Ctx, sel Selection, e ecs.Entity) {
	if lab := app.GetResource[Lab](c); lab != nil {
		lab.Sel, lab.Ent = sel, e
	}
	if ui := app.GetResource[UI](c); ui != nil {
		ui.Mode = ModeExplore
	}
}

// openGraph opens the node-graph editor on device e's chain (selecting it too so
// the state is consistent if capture mode is ever toggled off mid-run).
func openGraph(c *app.Ctx, sel Selection, e ecs.Entity) {
	selectMarker(c, sel, e)
	ui := app.GetResource[UI](c)
	ed := app.GetResource[Editor](c)
	if ui == nil || ed == nil {
		return
	}
	ed.target, ed.targetEnt, ed.editID, ed.redraw = sel, e, 0, true
	ui.Mode = ModeGraph
}
