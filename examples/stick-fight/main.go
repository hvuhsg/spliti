// stick-fight — two-player networked stick-figure fighting game.
//
// Run two terminals on one machine:
//
//	go run ./examples/stick-fight -mode=host   -addr=:7777
//	go run ./examples/stick-fight -mode=client -addr=localhost:7777
//
// Each peer steers its own fighter. Player 0 (green, left) faces right;
// player 1 (cyan, right) faces left. Land 3 hits to win.
//
// Controls (same on both peers — the network plugin tags each PlayerKey
// event with the sender's id so each press is routed to the right fighter):
//
//	a / d    move left / right
//	w        jump
//	s        crouch (held)
//	j        attack (height depends on posture: mid / low sweep / high kick)
//	k        defend (held — blocks any incoming hit)
//	r        restart after KO
//	q / Esc  quit (local)
//
// Hit resolution is height-aware. A standing punch hits at the mid row;
// crouching ducks under it; jumping rises above it. A crouch sweep hits
// low; a jumping kick hits high. Defending blocks at any height. All
// movement, animation frames, and combat resolution run in FixedUpdate
// off network.NetClock.Tick so both peers stay deterministically in sync.
package main

import (
	"flag"
	"fmt"
	"os"
	gotime "time"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/defaultplugins"
	"github.com/hvuhsg/spliti/plugin/input"
	"github.com/hvuhsg/spliti/plugin/network"
	splititime "github.com/hvuhsg/spliti/plugin/time"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// --- Stage geometry -----------------------------------------------------

const (
	boardW  = 60
	boardH  = 14
	hudRows = 2
	yGround = hudRows + boardH - 2 // legs sit on this row

	maxHealth = 3
	// All durations are in 60Hz ticks (~16.7ms each). The attack splits
	// into windup → strike → recover phases of roughly equal length; the
	// damage tick is the first tick of the strike phase.
	attackWindup    = 7  // ticks of windup before impact
	attackStrike    = 7  // ticks the strike pose holds
	attackRecover   = 7  // ticks of recover before the action ends
	attackDuration  = attackWindup + attackStrike + attackRecover
	attackImpact    = attackWindup
	attackReach     = 2
	defendLinger    = 12 // ~200ms after the last K press
	jumpDuration    = 24 // ~400ms airborne
	crouchLinger    = 12 // ~200ms after the last S press
	hitInvuln       = 18 // ~300ms i-frames after a hit
	idleHoldTicks   = 30 // hold each idle pose ~500ms
	defendHoldTicks = 12 // shimmer each defend frame ~200ms

	// Movement is latch-based so holding A/D feels continuous instead of
	// being gated by the OS auto-repeat curve. A fresh press grants a long
	// latch that bridges the OS initial-repeat delay; once we observe
	// rapid repeats we shorten the latch so releasing stops you quickly.
	moveCooldownTicks = 5  // one cell every ~83ms while latched (~12 cells/s)
	moveFreshLinger   = 36 // 600ms — bridges OS initial-repeat delay
	moveRepeatLinger  = 8  // ~133ms — short tail after release during a hold
	moveRepeatGap     = 4  // events arriving within this many ticks count as repeats

	maxParts = 9 // pre-spawned body-part entities per fighter
	hiddenX  = -1
)

// --- Posture / Action ---------------------------------------------------

type Posture int

const (
	PostureStand Posture = iota
	PostureCrouch
	PostureJump
)

type Action int

const (
	ActionNone Action = iota
	ActionAttack
	ActionDefend
)

// --- Fighter component / resource --------------------------------------

type Fighter struct {
	Owner network.PlayerID

	X      int // anchor column (mid-body)
	Facing int // +1 right, -1 left

	Posture     Posture
	Action      Action
	JumpTicks   int    // remaining airborne ticks (0 = grounded)
	ActionTicks int    // ticks elapsed within current Action
	CrouchHold  int    // ticks remaining of crouch latch
	DefendHold  int    // ticks remaining of defend latch
	AnimTick    uint64 // network tick at last posture/action change; drives idle cycle

	// Movement latch — driven by A/D presses, advanced in stepPhysics.
	MoveDir      int    // -1 = left, +1 = right, 0 = stopped
	MoveLinger   int    // ticks remaining of the auto-walk latch
	MoveCooldown int    // ticks until next auto-step
	LastMoveTick uint64 // tick of the last A/D press; used to detect OS repeats

	Health      int
	Alive       bool
	HitCooldown int

	Parts []ecs.Entity // length == maxParts; we reposition/reglyph each tick
}

type Fighters struct {
	Items []*Fighter
}

func (fs *Fighters) ByOwner(id network.PlayerID) *Fighter {
	for _, f := range fs.Items {
		if f != nil && f.Owner == id {
			return f
		}
	}
	return nil
}

// --- Game state ---------------------------------------------------------

type GameMode int

const (
	Playing GameMode = iota
	GameOver
)

type Match struct {
	Winner network.PlayerID
	Drawn  bool
}

// --- Glyph styles -------------------------------------------------------

var (
	p0Style    = tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)
	p0Hit      = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	p1Style    = tcell.StyleDefault.Foreground(tcell.ColorLightCyan).Bold(true)
	p1Hit      = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	groundGlyph = tui.Glyph{Char: '_', Style: tcell.StyleDefault.Foreground(tcell.ColorGray)}

	hudStyle  = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	overStyle = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	netStyle  = tcell.StyleDefault.Foreground(tcell.ColorOrange).Bold(true)
	hpFull    = tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)
	hpEmpty   = tcell.StyleDefault.Foreground(tcell.ColorDarkRed)
)

func styleForPlayer(id network.PlayerID, hit bool) tcell.Style {
	if hit {
		if id == 0 {
			return p0Hit
		}
		return p1Hit
	}
	if id == 0 {
		return p0Style
	}
	return p1Style
}

// --- Setup / teardown ---------------------------------------------------

func setupMatch(c *app.Ctx) {
	fs := app.GetResource[Fighters](c)
	mp := app.GetResource[Match](c)
	mp.Drawn = false
	mp.Winner = 0

	total := int(app.GetResource[network.LocalPlayer](c).Total)
	if total < 2 {
		total = 2
	}
	fs.Items = make([]*Fighter, total)

	mapper := generic.NewMap2[tui.Position, tui.Glyph](c.World())

	for pid := 0; pid < total; pid++ {
		var startX, facing int
		if pid%2 == 0 {
			startX, facing = boardW/4, +1
		} else {
			startX, facing = 3*boardW/4, -1
		}
		f := &Fighter{
			Owner:   network.PlayerID(pid),
			X:       startX,
			Facing:  facing,
			Posture: PostureStand,
			Action:  ActionNone,
			Health:  maxHealth,
			Alive:   true,
			Parts:   make([]ecs.Entity, maxParts),
		}
		for i := 0; i < maxParts; i++ {
			f.Parts[i] = mapper.NewWith(
				&tui.Position{X: hiddenX, Y: 0},
				&tui.Glyph{Char: ' ', Style: styleForPlayer(f.Owner, false)},
			)
		}
		fs.Items[pid] = f
	}

	// Ground line.
	for x := 0; x < boardW; x++ {
		mapper.NewWith(
			&tui.Position{X: x, Y: yGround + 1},
			&tui.Glyph{Char: groundGlyph.Char, Style: groundGlyph.Style},
		)
	}
}

func teardownMatch(c *app.Ctx) {
	fs := app.GetResource[Fighters](c)
	for _, f := range fs.Items {
		if f == nil {
			continue
		}
		for _, e := range f.Parts {
			if c.World().Alive(e) {
				c.World().RemoveEntity(e)
			}
		}
	}
	fs.Items = nil
	// Wipe ground / any other Position+Glyph entities not held by Fighters.
	var toRemove []ecs.Entity
	app.Query2[tui.Position, tui.Glyph](c, func(e ecs.Entity, _ *tui.Position, _ *tui.Glyph) {
		toRemove = append(toRemove, e)
	})
	for _, e := range toRemove {
		if c.World().Alive(e) {
			c.World().RemoveEntity(e)
		}
	}
}

// --- Systems: input -----------------------------------------------------

func handleLocalQuit(c *app.Ctx) {
	for _, ev := range app.ReadEvents[input.KeyEvent](c) {
		if ev.Key == tcell.KeyCtrlC || ev.Key == tcell.KeyEscape || ev.Rune == 'q' {
			c.App().Stop()
			return
		}
	}
}

// handleNetworkedInput drains network.PlayerKey events and applies each as
// a request on its owner's fighter. Runs in FixedUpdate so both peers see
// identical input sequences in identical order.
func handleNetworkedInput(c *app.Ctx) {
	state := app.GetState[GameMode](c)
	fs := app.GetResource[Fighters](c)
	clock := app.GetResource[network.NetClock](c)

	for _, ev := range app.ReadEvents[network.PlayerKey](c) {
		// Restart from GameOver: any peer can press 'r'.
		if (ev.Rune == 'r' || ev.Rune == 'R') && state.Get() == GameOver {
			state.Set(Playing)
			return
		}
		if state.Get() != Playing {
			continue
		}
		f := fs.ByOwner(ev.Player)
		if f == nil || !f.Alive {
			continue
		}
		// Posture (jump / crouch / move) and Action (attack / defend) are
		// independent axes — a fighter can run, jump, and punch all at the
		// same tick. We only gate within an axis: a new attack can't
		// interrupt an in-progress attack, and defending and attacking are
		// mutually exclusive on the action axis.
		attacking := f.Action == ActionAttack
		defending := f.Action == ActionDefend

		switch {
		case ev.Rune == 'a' || ev.Rune == 'A' || ev.Key == tcell.KeyLeft:
			latchMove(f, -1, clock.Tick)
		case ev.Rune == 'd' || ev.Rune == 'D' || ev.Key == tcell.KeyRight:
			latchMove(f, +1, clock.Tick)
		case ev.Rune == 'w' || ev.Rune == 'W' || ev.Key == tcell.KeyUp:
			// Jump from the ground at any time, including mid-attack.
			if f.Posture == PostureStand {
				f.Posture = PostureJump
				f.JumpTicks = jumpDuration
				f.AnimTick = clock.Tick
			}
		case ev.Rune == 's' || ev.Rune == 'S' || ev.Key == tcell.KeyDown:
			// Crouch any time on the ground, including mid-attack — the
			// attack continues from the new posture, retargeting low.
			if f.Posture != PostureJump {
				if f.Posture != PostureCrouch {
					f.AnimTick = clock.Tick
				}
				f.Posture = PostureCrouch
				f.CrouchHold = crouchLinger
			}
		case ev.Rune == 'j' || ev.Rune == 'J':
			// New attack — only blocked by an active attack or block.
			if !attacking && !defending {
				f.Action = ActionAttack
				f.ActionTicks = 0
				f.AnimTick = clock.Tick
			}
		case ev.Rune == 'k' || ev.Rune == 'K':
			// Block — refresh the latch each press; can't start a block
			// mid-swing.
			if !attacking {
				if !defending {
					f.AnimTick = clock.Tick
				}
				f.Action = ActionDefend
				f.DefendHold = defendLinger
			}
		}
	}
}

// --- Systems: physics ---------------------------------------------------

// latchMove arms the auto-walk latch in response to an A/D press. A fresh
// press (no recent same-direction event) gets the long latch so movement
// continues across the OS initial-repeat-delay gap; a tight repeat gets a
// short latch so releasing stops the fighter quickly.
func latchMove(f *Fighter, dir int, tick uint64) {
	if f.Posture == PostureCrouch {
		return
	}
	gap := tick - f.LastMoveTick
	if f.MoveDir == dir && f.LastMoveTick > 0 && gap <= moveRepeatGap {
		// Confirmed OS auto-repeat — short tail.
		if f.MoveLinger < moveRepeatLinger {
			f.MoveLinger = moveRepeatLinger
		}
	} else {
		// Fresh press or direction reversal — long latch + step on the
		// next physics tick.
		f.MoveLinger = moveFreshLinger
		f.MoveCooldown = 0
	}
	f.MoveDir = dir
	f.LastMoveTick = tick
}

// stepPhysics advances per-fighter timers: jump arc, crouch latch, defend
// latch, action timer, hit cooldown, and the auto-walk latch. It also
// re-orients each fighter to face the opponent so combat math is
// symmetric.
func stepPhysics(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	fs := app.GetResource[Fighters](c)

	// Re-orient facing toward the other fighter (deterministic: based on X).
	if len(fs.Items) >= 2 {
		a, b := fs.Items[0], fs.Items[1]
		if a != nil && b != nil {
			if a.X < b.X {
				a.Facing, b.Facing = +1, -1
			} else if a.X > b.X {
				a.Facing, b.Facing = -1, +1
			}
		}
	}

	for _, f := range fs.Items {
		if f == nil || !f.Alive {
			continue
		}

		// Auto-walk: while latched, advance one cell every
		// moveCooldownTicks. Crouching halts the walk; latch is cleared
		// so it doesn't resume when the fighter stands back up.
		if f.MoveLinger > 0 {
			if f.Posture == PostureCrouch {
				f.MoveLinger = 0
				f.MoveDir = 0
				f.MoveCooldown = 0
			} else {
				f.MoveLinger--
				f.MoveCooldown--
				if f.MoveCooldown <= 0 {
					nx := f.X + f.MoveDir
					if nx >= 1 && nx <= boardW-2 {
						f.X = nx
					}
					f.MoveCooldown = moveCooldownTicks
				}
				if f.MoveLinger <= 0 {
					f.MoveDir = 0
					f.MoveCooldown = 0
				}
			}
		}

		// Jump arc.
		if f.Posture == PostureJump {
			f.JumpTicks--
			if f.JumpTicks <= 0 {
				f.Posture = PostureStand
				f.JumpTicks = 0
			}
		}

		// Crouch latch — released when no S re-press refreshes the timer.
		if f.Posture == PostureCrouch {
			f.CrouchHold--
			if f.CrouchHold <= 0 {
				f.Posture = PostureStand
				f.CrouchHold = 0
			}
		}

		// Defend latch.
		if f.Action == ActionDefend {
			f.DefendHold--
			if f.DefendHold <= 0 {
				f.Action = ActionNone
				f.DefendHold = 0
			}
		}

		// Attack timer.
		if f.Action == ActionAttack {
			f.ActionTicks++
			if f.ActionTicks >= attackDuration {
				f.Action = ActionNone
				f.ActionTicks = 0
			}
		}

		if f.HitCooldown > 0 {
			f.HitCooldown--
		}
	}
}

// --- Systems: combat ----------------------------------------------------

// strikeRow returns the screen row an attacker's strike lands on, given
// the attacker's posture. Strikes connect at this exact row.
func strikeRow(p Posture) int {
	switch p {
	case PostureCrouch:
		return yGround // low sweep
	case PostureJump:
		return yGround - 2 // high kick
	default:
		return yGround - 1 // mid punch
	}
}

// hurtRows returns the set of rows occupied by the defender's torso/head
// for hit detection. Crouching tucks the body to the legs; jumping lifts
// it above the floor.
func hurtRows(p Posture) [3]int {
	switch p {
	case PostureCrouch:
		return [3]int{yGround, -1, -1} // only the legs cell is exposed
	case PostureJump:
		return [3]int{yGround - 3, yGround - 2, -1}
	default:
		return [3]int{yGround - 2, yGround - 1, yGround}
	}
}

func rowsContain(rows [3]int, target int) bool {
	for _, r := range rows {
		if r == target {
			return true
		}
	}
	return false
}

// resolveCombat applies damage on the impact tick of any active attack.
// Iteration is in fighter slice order; both peers reach the same result.
func resolveCombat(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	fs := app.GetResource[Fighters](c)
	if len(fs.Items) < 2 {
		return
	}

	for ai := 0; ai < len(fs.Items); ai++ {
		atk := fs.Items[ai]
		if atk == nil || !atk.Alive {
			continue
		}
		if atk.Action != ActionAttack || atk.ActionTicks != attackImpact {
			continue
		}
		row := strikeRow(atk.Posture)
		for di := 0; di < len(fs.Items); di++ {
			if di == ai {
				continue
			}
			def := fs.Items[di]
			if def == nil || !def.Alive {
				continue
			}
			// Range: defender must be within attackReach cells in the
			// direction the attacker is facing.
			dx := def.X - atk.X
			if atk.Facing > 0 && (dx <= 0 || dx > attackReach+1) {
				continue
			}
			if atk.Facing < 0 && (dx >= 0 || -dx > attackReach+1) {
				continue
			}
			if def.HitCooldown > 0 {
				continue
			}
			if def.Action == ActionDefend {
				continue // blocked
			}
			if !rowsContain(hurtRows(def.Posture), row) {
				continue // missed by height
			}
			def.Health--
			def.HitCooldown = hitInvuln
			// Knockback away from attacker, clamped to the field.
			if atk.Facing > 0 && def.X < boardW-2 {
				def.X++
			} else if atk.Facing < 0 && def.X > 1 {
				def.X--
			}
		}
	}
}

func checkGameOver(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	fs := app.GetResource[Fighters](c)
	mp := app.GetResource[Match](c)

	dead := []network.PlayerID{}
	alive := []network.PlayerID{}
	for _, f := range fs.Items {
		if f == nil {
			continue
		}
		if f.Health <= 0 {
			f.Alive = false
			dead = append(dead, f.Owner)
		} else {
			alive = append(alive, f.Owner)
		}
	}
	if len(dead) == 0 {
		return
	}
	if len(alive) == 1 {
		mp.Winner = alive[0]
		mp.Drawn = false
	} else {
		mp.Drawn = true
	}
	app.GetState[GameMode](c).Set(GameOver)
}

// --- Systems: render figures -------------------------------------------

// cell is a body-part glyph relative to the fighter's anchor (X, yGround).
type cell struct {
	dx, dy int
	ch     rune
}

// spriteCells returns the cells for a fighter's current posture/action,
// laid out for facing == +1. The renderer mirrors dx for facing == -1.
// Frame is derived from clock.Tick - f.AnimTick; passed in by the caller.
func spriteCells(f *Fighter, frame int) []cell {
	switch {
	case f.Action == ActionAttack:
		switch f.Posture {
		case PostureCrouch:
			return crouchAttackFrames[frame%len(crouchAttackFrames)]
		case PostureJump:
			return jumpAttackFrames[frame%len(jumpAttackFrames)]
		default:
			return standAttackFrames[frame%len(standAttackFrames)]
		}
	case f.Action == ActionDefend:
		return defendFrames[frame%len(defendFrames)]
	case f.Posture == PostureCrouch:
		return crouchFrames[frame%len(crouchFrames)]
	case f.Posture == PostureJump:
		// Frame split: rising vs falling (first half rises, second half falls).
		if f.JumpTicks > jumpDuration/2 {
			return jumpRisingFrames[0]
		}
		return jumpFallingFrames[0]
	default:
		return standIdleFrames[frame%len(standIdleFrames)]
	}
}

// All sprite tables. Coords are (dx, dy) relative to anchor (X, yGround),
// so dy = -2 is head row, dy = -1 is body row, dy = 0 is leg row.
//
// Right-facing layouts. The renderer flips dx (and certain glyphs) for
// left-facing fighters.
var (
	standIdleFrames = [][]cell{
		{
			{0, -2, 'O'},
			{-1, -1, '/'}, {0, -1, '|'}, {1, -1, '\\'},
			{-1, 0, '/'}, {1, 0, '\\'},
		},
		{
			{0, -2, 'O'},
			{-1, -1, '\\'}, {0, -1, '|'}, {1, -1, '/'},
			{-1, 0, '/'}, {1, 0, '\\'},
		},
	}
	crouchFrames = [][]cell{
		{
			{-1, -1, 'o'}, {0, -1, 'O'}, {1, -1, 'o'},
			{-1, 0, '/'}, {0, 0, '='}, {1, 0, '\\'},
		},
	}
	jumpRisingFrames = [][]cell{
		{
			{-1, -3, '\\'}, {0, -3, 'O'}, {1, -3, '/'},
			{0, -2, '|'},
			{-1, -1, '/'}, {1, -1, '\\'},
		},
	}
	jumpFallingFrames = [][]cell{
		{
			{-1, -3, '\\'}, {0, -3, 'O'}, {1, -3, '/'},
			{-1, -2, '-'}, {0, -2, '|'}, {1, -2, '-'},
			{-1, -1, '/'}, {1, -1, '\\'},
		},
	}
	standAttackFrames = [][]cell{
		// windup: arm cocked back
		{
			{0, -2, 'O'},
			{-1, -1, '\\'}, {0, -1, '|'}, {1, -1, '-'},
			{-1, 0, '/'}, {1, 0, '\\'},
		},
		// strike (impact tick): arm extended forward
		{
			{0, -2, 'O'},
			{-1, -1, '-'}, {0, -1, '|'}, {1, -1, '>'}, {2, -1, '>'},
			{-1, 0, '/'}, {1, 0, '\\'},
		},
		// recover
		{
			{0, -2, 'O'},
			{-1, -1, '\\'}, {0, -1, '|'}, {1, -1, '-'},
			{-1, 0, '/'}, {1, 0, '\\'},
		},
	}
	crouchAttackFrames = [][]cell{
		// windup
		{
			{-1, -1, 'o'}, {0, -1, 'O'}, {1, -1, 'o'},
			{-1, 0, '/'}, {0, 0, '='}, {1, 0, '\\'},
		},
		// strike — sweep low
		{
			{-1, -1, 'o'}, {0, -1, 'O'}, {1, -1, 'o'},
			{-1, 0, '/'}, {0, 0, '='}, {1, 0, '>'}, {2, 0, '>'},
		},
		// recover
		{
			{-1, -1, 'o'}, {0, -1, 'O'}, {1, -1, 'o'},
			{-1, 0, '/'}, {0, 0, '='}, {1, 0, '\\'},
		},
	}
	jumpAttackFrames = [][]cell{
		// windup
		{
			{-1, -3, '\\'}, {0, -3, 'O'}, {1, -3, '/'},
			{-1, -2, '-'}, {0, -2, '|'}, {1, -2, '-'},
			{-1, -1, '/'}, {1, -1, '\\'},
		},
		// strike — high kick extends sideways at body row
		{
			{-1, -3, '\\'}, {0, -3, 'O'}, {1, -3, '/'},
			{-1, -2, '-'}, {0, -2, '|'}, {1, -2, '>'}, {2, -2, '>'},
			{-1, -1, '/'}, {1, -1, '\\'},
		},
		// recover
		{
			{-1, -3, '\\'}, {0, -3, 'O'}, {1, -3, '/'},
			{-1, -2, '-'}, {0, -2, '|'}, {1, -2, '-'},
			{-1, -1, '/'}, {1, -1, '\\'},
		},
	}
	defendFrames = [][]cell{
		// guard up
		{
			{0, -2, 'O'},
			{-1, -1, '['}, {0, -1, '|'}, {1, -1, '-'},
			{-1, 0, '/'}, {1, 0, '\\'},
		},
		// guard shimmer
		{
			{0, -2, 'O'},
			{-1, -1, '['}, {0, -1, '|'}, {1, -1, ']'},
			{-1, 0, '/'}, {1, 0, '\\'},
		},
	}
)

// flipChar swaps direction-coupled glyphs when facing left.
func flipChar(r rune) rune {
	switch r {
	case '>':
		return '<'
	case '<':
		return '>'
	case '/':
		return '\\'
	case '\\':
		return '/'
	case '[':
		return ']'
	case ']':
		return '['
	default:
		return r
	}
}

// renderFigures writes each fighter's current sprite into its pre-spawned
// part entities. Unused parts are parked off-screen so the renderer skips
// them. Animation phase is derived purely from NetClock.Tick - AnimTick.
func renderFigures(c *app.Ctx) {
	fs := app.GetResource[Fighters](c)
	clock := app.GetResource[network.NetClock](c)
	posMap := generic.NewMap1[tui.Position](c.World())
	glyphMap := generic.NewMap1[tui.Glyph](c.World())

	for _, f := range fs.Items {
		if f == nil {
			continue
		}
		// Frame index — deterministic on both peers. Attack frames are
		// phase-mapped (windup/strike/recover) so the impact tick always
		// lands on the strike pose. Idle and defend hold each pose for
		// many ticks so the animation reads at human speed even at 60Hz.
		var frame int
		switch {
		case f.Action == ActionAttack:
			// Boundaries are exclusive on the low end so that the impact
			// tick (ActionTicks == attackImpact == attackWindup) renders
			// the strike pose, not the last windup pose.
			switch {
			case f.ActionTicks < attackWindup:
				frame = 0
			case f.ActionTicks < attackWindup+attackStrike:
				frame = 1
			default:
				frame = 2
			}
		case f.Action == ActionDefend:
			frame = int((clock.Tick - f.AnimTick) / defendHoldTicks)
		case f.Posture == PostureJump:
			// Rising vs falling is decided by JumpTicks inside spriteCells;
			// frame index is unused for jump.
			frame = 0
		case f.Posture == PostureCrouch:
			frame = 0
		default:
			frame = int((clock.Tick - f.AnimTick) / idleHoldTicks)
		}
		if frame < 0 {
			frame = 0
		}

		// Style: flash yellow during i-frames, dim while KO'd.
		hit := f.HitCooldown > 0
		st := styleForPlayer(f.Owner, hit)
		if !f.Alive {
			st = tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
		}

		cells := spriteCells(f, frame)
		for i := 0; i < maxParts; i++ {
			pos := posMap.Get(f.Parts[i])
			gly := glyphMap.Get(f.Parts[i])
			if i < len(cells) {
				dx := cells[i].dx
				ch := cells[i].ch
				if f.Facing < 0 {
					dx = -dx
					ch = flipChar(ch)
				}
				pos.X = f.X + dx
				pos.Y = yGround + cells[i].dy
				gly.Char = ch
				gly.Style = st
			} else {
				pos.X = hiddenX
				pos.Y = 0
				gly.Char = ' '
				gly.Style = st
			}
		}
	}
}

// --- HUD ----------------------------------------------------------------

func renderHUD(c *app.Ctx) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	fs := app.GetResource[Fighters](c)
	self := app.GetResource[network.LocalPlayer](c)
	netStat := app.GetResource[network.Status](c)
	mp := app.GetResource[Match](c)

	// Top HUD: title, network status hint, who-am-I.
	title := " SPLITI STICK-FIGHT "
	drawText(s, 1, 0, hudStyle, title)
	if len(fs.Items) >= 2 {
		me := fmt.Sprintf(" you = P%d ", self.ID)
		drawText(s, len(title)+2, 0, hudStyle, me)
	}

	// Per-player health bars on row 1.
	for i, f := range fs.Items {
		if f == nil {
			continue
		}
		label := fmt.Sprintf(" P%d ", f.Owner)
		var x int
		if i == 0 {
			x = 1
		} else {
			x = boardW - (len(label) + maxHealth + 2)
		}
		drawText(s, x, 1, styleForPlayer(f.Owner, false), label)
		x += len(label)
		for h := 0; h < maxHealth; h++ {
			var ch rune
			st := hpFull
			if h >= f.Health {
				ch = '.'
				st = hpEmpty
			} else {
				ch = '#'
			}
			s.SetContent(x+h, 1, ch, nil, st)
		}
	}

	if app.GetState[GameMode](c).Get() == GameOver {
		var msg string
		if mp.Drawn {
			msg = "  DOUBLE KO  "
		} else {
			msg = fmt.Sprintf("  P%d WINS  ", mp.Winner)
		}
		hint := "  press 'r' to restart, 'q' to quit  "
		w, _ := s.Size()
		drawText(s, (w-len(msg))/2, hudRows+boardH/2-1, overStyle, msg)
		drawText(s, (w-len(hint))/2, hudRows+boardH/2, overStyle, hint)
	}

	switch netStat.Phase {
	case network.Stalled:
		drawText(s, 1, hudRows+boardH+1, netStyle, " . network stall — waiting for opponent ")
	case network.PeerLost:
		drawText(s, 1, hudRows+boardH+1, netStyle, fmt.Sprintf(" x  player %d left ", netStat.LostPeer))
	}
}

func drawText(s tcell.Screen, x, y int, style tcell.Style, text string) {
	for i, r := range text {
		s.SetContent(x+i, y, r, nil, style)
	}
}

// --- Main ---------------------------------------------------------------

func main() {
	mode := flag.String("mode", "host", "host or client")
	addr := flag.String("addr", ":7777", "listen address (host) or remote address (client)")
	players := flag.Int("players", 2, "expected total players (host only)")
	flag.Parse()

	var nm network.Mode
	var listen, connect string
	switch *mode {
	case "host":
		nm = network.Host
		listen = *addr
	case "client":
		nm = network.Client
		connect = *addr
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q (want host or client)\n", *mode)
		flag.Usage()
		os.Exit(2)
	}

	a := app.New()
	a.AddPlugins(defaultplugins.Plugins{
		Time: splititime.Plugin{
			FixedTimestep:   gotime.Second / 60, // 60 Hz simulation tick
			TargetFrameRate: 60,
		},
	})
	a.AddPlugins(network.Plugin{
		Mode:    nm,
		Listen:  listen,
		Connect: connect,
		Players: *players,
	})

	app.InsertResource(a, &Fighters{})
	app.InsertResource(a, &Match{})
	app.InitState(a, Playing)

	app.OnEnter(a, Playing, setupMatch)
	app.OnExit(a, Playing, teardownMatch)

	a.AddSystems(schedule.Update, handleLocalQuit)
	a.AddSystems(schedule.FixedUpdate, app.Chain(
		app.System(handleNetworkedInput).Label("input"),
		app.System(stepPhysics).Label("physics"),
		app.System(resolveCombat).Label("combat"),
		app.System(checkGameOver).Label("gameover"),
		app.System(renderFigures).Label("draw"),
	))
	tui.AddOverlay(a, renderHUD)

	a.Run()
}
