package main

import (
	"math"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/input"
)

// handleInput drains key events each frame. Movement intents latch into Hold
// counters (consumed in movePlayer); fire and quit/restart act immediately.
func handleInput(c *app.Ctx) {
	state := app.GetState[GameMode](c)
	hold := app.GetResource[Hold](c)
	p := app.GetResource[Player](c)

	for _, ev := range app.ReadEvents[input.KeyEvent](c) {
		// Global quit.
		if ev.Key == tcell.KeyCtrlC || ev.Key == tcell.KeyEscape || ev.Rune == 'q' || ev.Rune == 'Q' {
			c.App().Stop()
			return
		}

		if state.Get() != Playing {
			// Only restart is meaningful while dead / victorious.
			if ev.Rune == 'r' || ev.Rune == 'R' {
				state.Set(Playing) // OnEnter(Playing) rebuilds the level
			}
			continue
		}

		switch {
		case ev.Key == tcell.KeyUp, ev.Rune == 'w', ev.Rune == 'W':
			hold.Fwd = holdTicks
		case ev.Key == tcell.KeyDown, ev.Rune == 's', ev.Rune == 'S':
			hold.Back = holdTicks
		case ev.Key == tcell.KeyLeft, ev.Rune == 'a', ev.Rune == 'A':
			hold.TurnL = holdTicks
		case ev.Key == tcell.KeyRight, ev.Rune == 'd', ev.Rune == 'D':
			hold.TurnR = holdTicks
		case ev.Rune == 'e', ev.Rune == 'E':
			hold.StrafeR = holdTicks
		case ev.Rune == ' ':
			p.FireQueued = true
		}
		// 'q' is quit above, so strafe-left uses a separate key.
		if ev.Rune == 'z' || ev.Rune == 'Z' {
			hold.StrafeL = holdTicks
		}
	}
}

// movePlayer applies the latched Hold intents for one fixed tick: rotation,
// forward/back along the direction vector, and strafing along the camera plane,
// each with wall-sliding collision. Then it decays the per-frame timers.
func movePlayer(c *app.Ctx) {
	p := app.GetResource[Player](c)
	hold := app.GetResource[Hold](c)
	g := app.GetResource[Game](c)

	// Rotation (rotate both the direction and the camera plane). The map's Y
	// axis points down, so a positive matrix rotation turns the view right;
	// turning left therefore uses the negative angle.
	if hold.TurnL > 0 {
		rotate(p, -turnSpeed)
		hold.TurnL--
	}
	if hold.TurnR > 0 {
		rotate(p, turnSpeed)
		hold.TurnR--
	}

	// Translation: accumulate this tick's desired move, then resolve collision.
	var dx, dy float64
	if hold.Fwd > 0 {
		dx += p.DirX * moveSpeed
		dy += p.DirY * moveSpeed
		hold.Fwd--
	}
	if hold.Back > 0 {
		dx -= p.DirX * moveSpeed
		dy -= p.DirY * moveSpeed
		hold.Back--
	}
	if hold.StrafeR > 0 {
		dx += p.PlaneX * strafeSpeed
		dy += p.PlaneY * strafeSpeed
		hold.StrafeR--
	}
	if hold.StrafeL > 0 {
		dx -= p.PlaneX * strafeSpeed
		dy -= p.PlaneY * strafeSpeed
		hold.StrafeL--
	}
	p.X, p.Y = moveWithCollision(g.Map, p.X, p.Y, dx, dy)

	// Per-tick timers.
	if p.FireCD > 0 {
		p.FireCD--
	}
	if p.MuzzleTicks > 0 {
		p.MuzzleTicks--
	}
	if p.HurtTicks > 0 {
		p.HurtTicks--
	}
}

// rotate turns the player's direction and camera-plane vectors by ang radians.
func rotate(p *Player, ang float64) {
	cos, sin := math.Cos(ang), math.Sin(ang)
	odx := p.DirX
	p.DirX = p.DirX*cos - p.DirY*sin
	p.DirY = odx*sin + p.DirY*cos
	opx := p.PlaneX
	p.PlaneX = p.PlaneX*cos - p.PlaneY*sin
	p.PlaneY = opx*sin + p.PlaneY*cos
}

// moveWithCollision advances (x,y) by (dx,dy), testing each axis independently
// so the mover slides along walls instead of sticking. A small radius keeps the
// body from clipping into the wall it's pressed against.
func moveWithCollision(m GameMap, x, y, dx, dy float64) (float64, float64) {
	const r = 0.2
	if dx > 0 && !m.solid(int(x+dx+r), int(y)) {
		x += dx
	} else if dx < 0 && !m.solid(int(x+dx-r), int(y)) {
		x += dx
	}
	if dy > 0 && !m.solid(int(x), int(y+dy+r)) {
		y += dy
	} else if dy < 0 && !m.solid(int(x), int(y+dy-r)) {
		y += dy
	}
	return x, y
}
