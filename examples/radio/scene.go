package main

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/webgpu"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// Scene is the high-level screen the game is showing. Each value is a state in
// the engine's state machine; OnEnter spawns that screen's entities and OnExit
// tears them down. The detail scenes are reached by clicking a stage in MainFlow.
type Scene int

const (
	MainFlow      Scene = iota // the end-to-end pipeline overview
	Constellation              // bits -> QPSK symbols (complex plane)
	IQWave                     // symbols -> I/Q wave (+ pseudo-3D helix)
	Channel                    // over-the-air (noise)
	Demod                      // wave -> symbols (demodulation)
	Decode                     // symbols -> bits (decision)
	Message                    // a full multi-symbol message, end to end
	QAM16                      // 16-QAM constellation (4 bits/symbol)
	Compose                    // type your own message and see its wave
	MultiFreq                  // multi-frequency (OFDM) overview
)

// Layer Z bands keep draw order stable regardless of archetype iteration order:
// backgrounds at the bottom, interactive buttons on top.
const (
	zPanel  = 0
	zAxis   = 10
	zRing   = 11
	zDot    = 20
	zPacket = 25
	zLabel  = 30
	zButton = 40
	zBtnTxt = 41
)

// Palette. Textures are white, so Color is what you actually see.
var (
	colPanel  = webgpu.Color{R: 0.11, G: 0.13, B: 0.19, A: 1}
	colPanel2 = webgpu.Color{R: 0.16, G: 0.19, B: 0.27, A: 1}
	colAxis   = webgpu.Color{R: 0.40, G: 0.45, B: 0.55, A: 1}
	colGrid   = webgpu.Color{R: 0.22, G: 0.26, B: 0.34, A: 1}
	colAccent = webgpu.Color{R: 0.20, G: 0.62, B: 0.96, A: 1}
	colI      = webgpu.Color{R: 0.35, G: 0.78, B: 0.42, A: 1} // green: in-phase
	colQ      = webgpu.Color{R: 0.97, G: 0.45, B: 0.32, A: 1} // orange: quadrature
	colSum    = webgpu.Color{R: 1.00, G: 0.78, B: 0.16, A: 1} // amber: combined
	colText   = webgpu.Color{R: 0.90, G: 0.93, B: 0.97, A: 1}
	colMuted  = webgpu.Color{R: 0.48, G: 0.54, B: 0.64, A: 1}
	colHi     = webgpu.Color{R: 1, G: 1, B: 1, A: 1}
	colErr    = webgpu.Color{R: 0.95, G: 0.30, B: 0.25, A: 1} // wrong decision / bit error
)

// stateIs returns a RunIf condition true only while the given scene is active.
func stateIs(s Scene) func(*app.Ctx) bool {
	return func(c *app.Ctx) bool { return app.GetState[Scene](c).Get() == s }
}

// teardownScene despawns every visual entity. The example spawns nothing without
// a Sprite, and no Sprite entities live across scenes (shared art lives in the
// TextureRegistry, not in entities), so clearing all Sprite entities is the whole
// cleanup. Despawns are deferred and flushed before the next scene's OnEnter
// spawns are applied, so there is no overlap.
func teardownScene(c *app.Ctx) {
	var ents []ecs.Entity
	app.Query1[webgpu.Sprite](c, func(e ecs.Entity, _ *webgpu.Sprite) {
		ents = append(ents, e)
	})
	for _, e := range ents {
		c.Commands().Despawn(e)
	}
}

// --- spawn helpers -----------------------------------------------------------

// spawnSprite creates a plain visual quad (Transform+Sprite+Color+Layer).
func spawnSprite(cmds *app.Commands, ref string, x, y, w, h float32, col webgpu.Color, z int) {
	app.Spawn4[webgpu.Transform, webgpu.Sprite, webgpu.Color, webgpu.Layer](cmds,
		func(t *webgpu.Transform, s *webgpu.Sprite, c *webgpu.Color, l *webgpu.Layer) {
			t.X, t.Y, t.W, t.H = x, y, w, h
			s.Ref = ref
			*c = col
			l.Z = z
		})
}

// spawn5 is the 5-component analogue of app.Spawn4, used for entities that also
// carry a Clickable or a Dot tag.
func spawn5[A, B, C, D, E any](cmds *app.Commands, init func(*A, *B, *C, *D, *E)) {
	cmds.Add(func(w *ecs.World) {
		m := generic.NewMap5[A, B, C, D, E](w)
		e := m.New()
		if init != nil {
			a, b, c, d, ee := m.Get(e)
			init(a, b, c, d, ee)
		}
	})
}

// spawnDot creates one curve point belonging to a plot series.
func spawnDot(cmds *app.Commands, series, index, count int, size float32, col webgpu.Color) {
	spawn5[webgpu.Transform, webgpu.Sprite, webgpu.Color, webgpu.Layer, Dot](cmds,
		func(t *webgpu.Transform, s *webgpu.Sprite, c *webgpu.Color, l *webgpu.Layer, d *Dot) {
			t.W, t.H = size, size
			t.X, t.Y = -10, -10 // parked off-screen until a draw system places it
			s.Ref = "dot"
			*c = col
			l.Z = zDot
			d.Series, d.Index, d.Count = series, index, count
		})
}
