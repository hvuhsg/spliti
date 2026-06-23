package systems

import (
	"math"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/inputs"
	"github.com/hvuhsg/spliti/plugin/inputs/actions"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	splititime "github.com/hvuhsg/spliti/plugin/time"

	"hexisland/game/wfc"
)

// Player-controller tuning. The world is built from unit-scale hex tiles, so
// these are in tile-sized units per second.
const (
	EyeHeight    = 0.6  // camera height above the feet position
	PlayerRadius = 0.25 // for the island wall clamp

	MoveSpeed      = 4.0  // ground target speed
	SprintMult     = 1.7  // sprint multiplier
	GroundAccel    = 60.0 // how fast ground velocity approaches the target
	GroundFriction = 10.0 // decel when no input on the ground
	AirAccel       = 12.0 // Quake-style air acceleration (enables strafe/bunny-hop)
	Gravity        = 18.0
	JumpVel        = 6.0
	CoyoteTime     = 0.10 // grace period to still jump just after leaving ground
	JumpBufTime    = 0.10 // remember a jump press for this long before landing

	MouseSens  = 0.0022 // radians of look per pixel of mouse motion
	PitchLimit = 1.5    // ~86°, just short of straight up/down

	StepHeight = 0.35 // max height the feet snap up when walking onto a ledge
)

// Player is the first-person controller state and the contract the combat
// systems read (position, look direction, eye, health).
type Player struct {
	Pos      m.Vec3  // feet position (ground contact point)
	Vel      m.Vec3  // world-space velocity, units/sec
	Yaw      float32 // radians around +Y; 0 looks toward -Z
	Pitch    float32 // radians, clamped to ±PitchLimit
	Grounded bool
	Health   float32
	Captured bool // whether the mouse is currently locked for mouselook

	coyote  float32
	jumpBuf float32
	recoil  float32 // transient upward view kick from firing, decays to 0
}

// NewPlayer builds the initial player state. SpawnPlayer positions it on the
// island once the world exists.
func NewPlayer() *Player { return &Player{Health: 100} }

// Eye is the camera/muzzle origin: the feet position lifted to eye height.
func (p *Player) Eye() m.Vec3 { return p.Pos.Add(m.Vec3{Y: EyeHeight}) }

// Forward is the unit look direction from yaw and pitch (including the transient
// recoil kick, so the crosshair and the hitscan stay aligned while the gun rises).
func (p *Player) Forward() m.Vec3 {
	pitch := clampF(p.Pitch+p.recoil, -PitchLimit, PitchLimit)
	cp, sp := cosf(pitch), sinf(pitch)
	sy, cy := sinf(p.Yaw), cosf(p.Yaw)
	return m.Vec3{X: cp * sy, Y: sp, Z: -cp * cy}
}

// SpawnPlayer drops the player onto a walkable cell near the island centre. It
// runs at startup after GenerateWorld; the tile entities are not spawned yet
// (they are queued through Commands), so the player starts a little above the
// surface and the first physics step settles it onto the ground.
func SpawnPlayer(c *app.Ctx) {
	p := app.GetResource[Player](c)
	g := app.GetResource[Game](c)
	if p == nil || g == nil {
		return
	}
	best := wfc.Coord{Q: 0, R: 0}
	bestD := math.MaxFloat64
	for coord, cell := range g.Board.Cells {
		if cell == nil || !cell.Collapsed || wfc.Tiles[cell.Tile].Terrain == wfc.Water {
			continue
		}
		x, z := g.Board.WorldXZ(coord)
		if d := float64(x*x + z*z); d < bestD {
			bestD, best = d, coord
		}
	}
	x, z := g.Board.WorldXZ(best)
	p.Pos = m.Vec3{X: x, Y: 3, Z: z}
	p.Vel = m.Vec3{}
	p.Yaw, p.Pitch = 0, 0
	p.Health = 100
}

// PlayerInput samples per-frame input: it captures/releases the mouse, applies
// mouselook from the relative mouse delta, and buffers a jump press. Runs in
// Update; the physics in PlayerMove (FixedUpdate) consumes the buffer.
func PlayerInput(c *app.Ctx) {
	p := app.GetResource[Player](c)
	if p == nil {
		return
	}

	// Capture the cursor on the first click for mouselook — but never inside the
	// editor, where the cursor belongs to the editor UI.
	inEditor := app.GetResource[app.EditorMode](c) != nil
	if !p.Captured && !inEditor && render3d.MouseButtonDown(c, inputs.MouseButtonLeft) {
		render3d.SetMouseCaptured(c, true)
		p.Captured = true
	}
	if p.Captured && render3d.KeyDown(c, inputs.KeyEscape) {
		render3d.SetMouseCaptured(c, false)
		p.Captured = false
	}

	// Mouselook only while captured, so an un-captured cursor doesn't spin the view.
	if p.Captured {
		dx, dy := render3d.MouseDelta(c)
		p.Yaw += float32(dx) * MouseSens // mouse right turns the view right
		p.Pitch = clampF(p.Pitch-float32(dy)*MouseSens, -PitchLimit, PitchLimit)
	}

	if acts := actions.Get(c); acts != nil && acts.JustPressed("jump") {
		p.jumpBuf = JumpBufTime
	}

	// Recover the firing recoil toward zero.
	if tm := app.GetResource[splititime.Time](c); tm != nil {
		dt := float32(tm.Delta().Seconds())
		p.recoil = approach(p.recoil, 0, RecoilRecover*RecoilKick*dt)
	}
}

// approach moves v toward target by at most step.
func approach(v, target, step float32) float32 {
	if v < target {
		return minF(target, v+step)
	}
	return maxF(target, v-step)
}

// PlayerMove integrates the controller at a fixed timestep: acceleration and
// friction, gravity, coyote-time/buffered jumping, then ground, water, and the
// island-wall resolution. The Quake-style air acceleration is what gives the
// movement its strafe/air-control feel.
func PlayerMove(c *app.Ctx) {
	p := app.GetResource[Player](c)
	g := app.GetResource[Game](c)
	acts := actions.Get(c)
	tm := app.GetResource[splititime.Time](c)
	if p == nil || g == nil || acts == nil || tm == nil {
		return
	}
	dt := float32(tm.FixedDelta().Seconds())

	EnsureHeights(c, g) // builds the surface height map on the first tick

	if !playing(c) {
		return // freeze the body on death; the camera still works to look around
	}

	// Horizontal movement basis from yaw (at yaw 0, forward is -Z).
	sy, cy := sinf(p.Yaw), cosf(p.Yaw)
	fwd := m.Vec3{X: sy, Z: -cy}
	right := m.Vec3{X: cy, Z: sy}
	wish := right.Scale(acts.Axis("move-x")).Add(fwd.Scale(acts.Axis("move-y")))
	wish.Y = 0
	if wish.LengthSq() > 1 {
		wish = wish.Normalize()
	}

	speed := float32(MoveSpeed)
	if acts.Held("sprint") {
		speed *= SprintMult
	}

	horiz := m.Vec3{X: p.Vel.X, Z: p.Vel.Z}
	if p.Grounded {
		if wish.LengthSq() > 1e-6 {
			target := wish.Scale(speed)
			horiz = horiz.Add(target.Sub(horiz).Scale(clampF(GroundAccel*dt, 0, 1)))
		} else {
			horiz = horiz.Scale(maxF(0, 1-GroundFriction*dt))
		}
	} else if wish.LengthSq() > 1e-6 {
		// Air control: add velocity along wish only up to the projected cap, so
		// total air speed can exceed MoveSpeed by strafing but never runs away.
		if add := speed - horiz.Dot(wish); add > 0 {
			horiz = horiz.Add(wish.Scale(minF(AirAccel*speed*dt, add)))
		}
	}
	p.Vel.X, p.Vel.Z = horiz.X, horiz.Z

	p.Vel.Y -= Gravity * dt

	if p.Grounded {
		p.coyote = CoyoteTime
	} else {
		p.coyote = maxF(0, p.coyote-dt)
	}
	p.jumpBuf = maxF(0, p.jumpBuf-dt)
	if p.jumpBuf > 0 && p.coyote > 0 {
		p.Vel.Y = JumpVel
		p.jumpBuf, p.coyote = 0, 0
		p.Grounded = false
	}

	prevX, prevZ := p.Pos.X, p.Pos.Z
	p.Pos = p.Pos.Add(p.Vel.Scale(dt))

	resolveGround(g, p)
	handleWater(g, p, prevX, prevZ)
	clampToIsland(p)
}

// resolveGround snaps the feet to the sampled tile surface beneath them (see
// EnsureHeights), landing the player and zeroing downward velocity.
func resolveGround(g *Game, p *Player) {
	groundY, ok := GroundHeightAt(g, p.Pos.X, p.Pos.Z)
	if !ok {
		p.Grounded = false
		return
	}
	switch {
	case p.Pos.Y <= groundY:
		p.Pos.Y = groundY
		if p.Vel.Y < 0 {
			p.Vel.Y = 0
		}
		p.Grounded = true
	case p.Grounded && p.Pos.Y-groundY < StepHeight && p.Vel.Y <= 0:
		// Walking down a small step/slope: stick to the surface.
		p.Pos.Y = groundY
		p.Vel.Y = 0
		p.Grounded = true
	default:
		p.Grounded = false
	}
}

// handleWater keeps the player off the sea: stepping onto a water tile restores
// the previous horizontal position and stops horizontal motion, so the shoreline
// acts as a wall. The water-ringed island plus this rule fence the player to land.
func handleWater(g *Game, p *Player, prevX, prevZ float32) {
	coord := wfc.FromWorld(p.Pos.X, p.Pos.Z)
	cell := g.Board.Cells[coord]
	if cell != nil && cell.Collapsed && wfc.Tiles[cell.Tile].Terrain == wfc.Water {
		p.Pos.X, p.Pos.Z = prevX, prevZ
		p.Vel.X, p.Vel.Z = 0, 0
	}
}

// clampToIsland is the authoritative containment: it keeps the player inside the
// dome radius and removes the outward velocity component so you slide along the
// wall instead of sticking to it.
func clampToIsland(p *Player) {
	d := float32(math.Hypot(float64(p.Pos.X), float64(p.Pos.Z)))
	maxR := IslandRadius - PlayerRadius
	if d <= maxR || d < 1e-6 {
		return
	}
	p.Pos.X *= maxR / d
	p.Pos.Z *= maxR / d
	nx, nz := p.Pos.X/d, p.Pos.Z/d // outward unit normal (pre-scale direction)
	if vdot := p.Vel.X*nx + p.Vel.Z*nz; vdot > 0 {
		p.Vel.X -= nx * vdot
		p.Vel.Z -= nz * vdot
	}
}

// PlayerCamera drives the view from the player pose. Runs after PlayerInput so
// the look direction is current, and after PlayerMove (FixedUpdate, earlier in
// the frame) so the eye reflects this frame's movement.
func PlayerCamera(c *app.Ctx) {
	p := app.GetResource[Player](c)
	cam := app.GetResource[render3d.Camera3D](c)
	if p == nil || cam == nil {
		return
	}
	eye := p.Eye()
	cam.Position = eye
	cam.Target = eye.Add(p.Forward())
	cam.Up = m.Vec3{Y: 1}
	cam.FovYDeg = 78
	cam.Near = 0.05
}

func sinf(x float32) float32 { return float32(math.Sin(float64(x))) }
func cosf(x float32) float32 { return float32(math.Cos(float64(x))) }

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
