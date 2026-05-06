// stick-fight-ai — single-player stick-figure fighter against an AI opponent.
//
// Same fighter model, height-aware combat, and animation system as
// `examples/stick-fight`, but with no networking. Player 0 is the human,
// reading keys directly from input.KeyEvent. Player 1 is driven by an
// `aiInput` system that decides what to "press" each fixed tick by
// evaluating distance, the human's current action/posture, and a small
// dose of randomness.
//
// Run:
//
//	go run ./examples/stick-fight-ai
//
// Controls:
//
//	a / d    move left / right
//	w        jump
//	s        crouch (held)
//	j        attack (height depends on posture: mid / low sweep / high kick)
//	k        defend (held — blocks any incoming hit)
//	r        restart after KO
//	q / Esc  quit
package main

import (
	mathrand "math/rand"
	gotime "time"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/defaultplugins"
	"github.com/hvuhsg/spliti/plugin/input"
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
	yGround = hudRows + boardH - 2

	maxHealth       = 3
	attackWindup    = 7
	attackStrike    = 7
	attackRecover   = 7
	attackDuration  = attackWindup + attackStrike + attackRecover
	attackImpact    = attackWindup
	attackReach     = 2
	defendLinger    = 12
	jumpDuration    = 24
	crouchLinger    = 12
	hitInvuln       = 18
	idleHoldTicks   = 30
	defendHoldTicks = 12

	moveCooldownTicks = 5
	moveFreshLinger   = 36
	moveRepeatLinger  = 8
	moveRepeatGap     = 4

	maxParts = 9
	hiddenX  = -1

	// Player IDs — purely a labelling convention here, no networking.
	humanID = 0
	aiID    = 1
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

// --- Fighter -----------------------------------------------------------

type Fighter struct {
	Owner int

	X      int
	Facing int

	Posture     Posture
	Action      Action
	JumpTicks   int
	ActionTicks int
	CrouchHold  int
	DefendHold  int
	AnimTick    uint64

	MoveDir      int
	MoveLinger   int
	MoveCooldown int
	LastMoveTick uint64

	Health      int
	Alive       bool
	HitCooldown int

	Parts []ecs.Entity
}

type Fighters struct {
	Items []*Fighter
}

func (fs *Fighters) ByOwner(id int) *Fighter {
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
	Winner int
	Drawn  bool
}

// LocalClock is a non-networked tick counter, advanced once per
// FixedUpdate. Animations and the AI brain key off this so behaviour is
// independent of frame rate.
type LocalClock struct {
	Tick uint64
}

// AIBrain holds per-match state for the opponent's controller. The RNG
// is seeded once per match so behaviour varies between runs but is
// reproducible within a single match.
type AIBrain struct {
	rng *mathrand.Rand
}

// --- Glyph styles -------------------------------------------------------

var (
	p0Style     = tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)
	p1Style     = tcell.StyleDefault.Foreground(tcell.ColorLightCyan).Bold(true)
	hitStyle    = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	groundGlyph = tui.Glyph{Char: '_', Style: tcell.StyleDefault.Foreground(tcell.ColorGray)}

	hudStyle  = tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)
	overStyle = tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
	hpFull    = tcell.StyleDefault.Foreground(tcell.ColorLightGreen).Bold(true)
	hpEmpty   = tcell.StyleDefault.Foreground(tcell.ColorDarkRed)
)

func styleForFighter(f *Fighter, hit bool) tcell.Style {
	if hit {
		return hitStyle
	}
	if f.Owner == humanID {
		return p0Style
	}
	return p1Style
}

// --- Setup / teardown ---------------------------------------------------

func setupMatch(c *app.Ctx) {
	fs := app.GetResource[Fighters](c)
	mp := app.GetResource[Match](c)
	clock := app.GetResource[LocalClock](c)
	brain := app.GetResource[AIBrain](c)

	mp.Drawn = false
	mp.Winner = 0
	clock.Tick = 0
	brain.rng = mathrand.New(mathrand.NewSource(gotime.Now().UnixNano()))

	fs.Items = make([]*Fighter, 2)
	mapper := generic.NewMap2[tui.Position, tui.Glyph](c.World())

	specs := []struct {
		owner  int
		startX int
		facing int
	}{
		{humanID, boardW / 4, +1},
		{aiID, 3 * boardW / 4, -1},
	}
	for i, s := range specs {
		f := &Fighter{
			Owner:   s.owner,
			X:       s.startX,
			Facing:  s.facing,
			Posture: PostureStand,
			Action:  ActionNone,
			Health:  maxHealth,
			Alive:   true,
			Parts:   make([]ecs.Entity, maxParts),
		}
		for p := 0; p < maxParts; p++ {
			f.Parts[p] = mapper.NewWith(
				&tui.Position{X: hiddenX, Y: 0},
				&tui.Glyph{Char: ' ', Style: styleForFighter(f, false)},
			)
		}
		fs.Items[i] = f
	}

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

// --- Action helpers (shared between human input and AI) -----------------

// latchMove arms the auto-walk latch in response to an A/D-equivalent
// press. A fresh press grants a long latch that bridges the OS
// initial-repeat-delay gap; tight repeats get a short tail so releasing
// stops the fighter quickly. The AI calls this every tick it wants to
// keep walking, so its movement always uses the short tail.
func latchMove(f *Fighter, dir int, tick uint64) {
	if f.Posture == PostureCrouch {
		return
	}
	gap := tick - f.LastMoveTick
	if f.MoveDir == dir && f.LastMoveTick > 0 && gap <= moveRepeatGap {
		if f.MoveLinger < moveRepeatLinger {
			f.MoveLinger = moveRepeatLinger
		}
	} else {
		f.MoveLinger = moveFreshLinger
		f.MoveCooldown = 0
	}
	f.MoveDir = dir
	f.LastMoveTick = tick
}

func tryJump(f *Fighter, tick uint64) {
	if f.Posture == PostureStand {
		f.Posture = PostureJump
		f.JumpTicks = jumpDuration
		f.AnimTick = tick
	}
}

func tryCrouch(f *Fighter, tick uint64) {
	if f.Posture == PostureJump {
		return
	}
	if f.Posture != PostureCrouch {
		f.AnimTick = tick
	}
	f.Posture = PostureCrouch
	f.CrouchHold = crouchLinger
}

func tryAttack(f *Fighter, tick uint64) {
	if f.Action != ActionNone {
		return
	}
	f.Action = ActionAttack
	f.ActionTicks = 0
	f.AnimTick = tick
}

func tryDefend(f *Fighter, tick uint64) {
	if f.Action == ActionAttack {
		return
	}
	if f.Action != ActionDefend {
		f.AnimTick = tick
	}
	f.Action = ActionDefend
	f.DefendHold = defendLinger
}

// --- Systems: clock & input ---------------------------------------------

func tickClock(c *app.Ctx) {
	app.GetResource[LocalClock](c).Tick++
}

func handleLocalQuit(c *app.Ctx) {
	for _, ev := range app.ReadEvents[input.KeyEvent](c) {
		if ev.Key == tcell.KeyCtrlC || ev.Key == tcell.KeyEscape || ev.Rune == 'q' {
			c.App().Stop()
			return
		}
	}
}

// handleHumanInput drains input.KeyEvent and applies each press to the
// human-controlled fighter. The 'r' restart from GameOver is wired here
// since it's user-driven; quit is handled separately in Update so it
// works even with the simulation paused.
func handleHumanInput(c *app.Ctx) {
	state := app.GetState[GameMode](c)
	fs := app.GetResource[Fighters](c)
	clock := app.GetResource[LocalClock](c)

	f := fs.ByOwner(humanID)

	for _, ev := range app.ReadEvents[input.KeyEvent](c) {
		if (ev.Rune == 'r' || ev.Rune == 'R') && state.Get() == GameOver {
			state.Set(Playing)
			return
		}
		if state.Get() != Playing || f == nil || !f.Alive {
			continue
		}
		attacking := f.Action == ActionAttack
		defending := f.Action == ActionDefend

		switch {
		case ev.Rune == 'a' || ev.Rune == 'A' || ev.Key == tcell.KeyLeft:
			latchMove(f, -1, clock.Tick)
		case ev.Rune == 'd' || ev.Rune == 'D' || ev.Key == tcell.KeyRight:
			latchMove(f, +1, clock.Tick)
		case ev.Rune == 'w' || ev.Rune == 'W' || ev.Key == tcell.KeyUp:
			tryJump(f, clock.Tick)
		case ev.Rune == 's' || ev.Rune == 'S' || ev.Key == tcell.KeyDown:
			tryCrouch(f, clock.Tick)
		case ev.Rune == 'j' || ev.Rune == 'J':
			if !attacking && !defending {
				tryAttack(f, clock.Tick)
			}
		case ev.Rune == 'k' || ev.Rune == 'K':
			if !attacking {
				tryDefend(f, clock.Tick)
			}
		}
	}
}

// --- Systems: AI --------------------------------------------------------

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// aiInput decides Player 1's actions for this tick. It reads the human's
// current state and reacts: defend or jump when an attack is winding up,
// approach when out of range, mix mid/low/high attacks when in range, and
// occasionally back away. The AI is not perfect on purpose — reaction
// rolls leave gaps where it gets hit.
func aiInput(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	fs := app.GetResource[Fighters](c)
	if len(fs.Items) < 2 {
		return
	}
	ai := fs.ByOwner(aiID)
	human := fs.ByOwner(humanID)
	if ai == nil || human == nil || !ai.Alive || !human.Alive {
		return
	}
	clock := app.GetResource[LocalClock](c)
	brain := app.GetResource[AIBrain](c)
	if brain.rng == nil {
		brain.rng = mathrand.New(mathrand.NewSource(gotime.Now().UnixNano()))
	}
	r := brain.rng

	dist := absInt(human.X - ai.X)
	dir := +1
	if human.X < ai.X {
		dir = -1
	}

	// React to the human's incoming attack: only the windup phase is
	// reactable — once they're past the impact tick the hit either
	// already landed or already missed.
	humanThreatening := human.Action == ActionAttack &&
		human.ActionTicks < attackImpact &&
		dist <= attackReach+2

	if humanThreatening && ai.Action == ActionNone && ai.HitCooldown == 0 {
		// Pick a defensive response. The probabilities leave a 15%
		// "frozen" chance so the AI sometimes eats the hit.
		switch roll := r.Intn(100); {
		case roll < 55:
			tryDefend(ai, clock.Tick)
			return
		case roll < 70:
			// Try to jump out — works as a dodge against a mid punch
			// (jump rises above yGround-1 to {head,body} above it).
			tryJump(ai, clock.Tick)
			return
		case roll < 85:
			// Duck — works against a high kick (crouch hurtbox is the
			// legs row only).
			tryCrouch(ai, clock.Tick)
			return
		}
	}

	// Don't queue new posture/action work while attacking — the attack
	// must complete. (We could move during it; see below.)
	midAttack := ai.Action == ActionAttack

	// Approach when out of range, walk away when too close on cooldown.
	switch {
	case dist > attackReach+1:
		// Out of range — close the gap. Refresh the move latch every
		// tick we want to keep walking.
		latchMove(ai, dir, clock.Tick)
	case dist <= 1 && ai.Action == ActionNone && r.Intn(100) < 8:
		// Crowded — back off briefly so the next attack has spacing.
		latchMove(ai, -dir, clock.Tick)
	}

	// In range and not currently committed — roll for an attack opener.
	// Probability is low per tick so the AI doesn't mash; over a 350ms
	// attack cycle plus the pause for Action to clear, this works out to
	// roughly 1 swing every ~600ms on average.
	if !midAttack && dist <= attackReach+1 && ai.HitCooldown == 0 {
		if r.Intn(100) < 5 {
			switch roll := r.Intn(100); {
			case roll < 60:
				// Standing punch (mid). Stops walking; the attack is
				// the priority now.
				tryAttack(ai, clock.Tick)
			case roll < 80:
				// Jump kick (high) — same-tick combo: jump first, then
				// attack. spriteCells will pick jumpAttackFrames since
				// Posture is now Jump and Action is Attack.
				tryJump(ai, clock.Tick)
				tryAttack(ai, clock.Tick)
			default:
				// Low sweep — same-tick crouch + attack.
				tryCrouch(ai, clock.Tick)
				tryAttack(ai, clock.Tick)
			}
		}
	}
}

// --- Systems: physics ---------------------------------------------------

func stepPhysics(c *app.Ctx) {
	if app.GetState[GameMode](c).Get() != Playing {
		return
	}
	fs := app.GetResource[Fighters](c)

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

		if f.Posture == PostureJump {
			f.JumpTicks--
			if f.JumpTicks <= 0 {
				f.Posture = PostureStand
				f.JumpTicks = 0
			}
		}
		if f.Posture == PostureCrouch {
			f.CrouchHold--
			if f.CrouchHold <= 0 {
				f.Posture = PostureStand
				f.CrouchHold = 0
			}
		}
		if f.Action == ActionDefend {
			f.DefendHold--
			if f.DefendHold <= 0 {
				f.Action = ActionNone
				f.DefendHold = 0
			}
		}
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

func strikeRow(p Posture) int {
	switch p {
	case PostureCrouch:
		return yGround
	case PostureJump:
		return yGround - 2
	default:
		return yGround - 1
	}
}

func hurtRows(p Posture) [3]int {
	switch p {
	case PostureCrouch:
		return [3]int{yGround, -1, -1}
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
				continue
			}
			if !rowsContain(hurtRows(def.Posture), row) {
				continue
			}
			def.Health--
			def.HitCooldown = hitInvuln
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

	dead := []int{}
	alive := []int{}
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

// --- Sprite tables ------------------------------------------------------

type cell struct {
	dx, dy int
	ch     rune
}

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
		if f.JumpTicks > jumpDuration/2 {
			return jumpRisingFrames[0]
		}
		return jumpFallingFrames[0]
	default:
		return standIdleFrames[frame%len(standIdleFrames)]
	}
}

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
		{
			{0, -2, 'O'},
			{-1, -1, '\\'}, {0, -1, '|'}, {1, -1, '-'},
			{-1, 0, '/'}, {1, 0, '\\'},
		},
		{
			{0, -2, 'O'},
			{-1, -1, '-'}, {0, -1, '|'}, {1, -1, '>'}, {2, -1, '>'},
			{-1, 0, '/'}, {1, 0, '\\'},
		},
		{
			{0, -2, 'O'},
			{-1, -1, '\\'}, {0, -1, '|'}, {1, -1, '-'},
			{-1, 0, '/'}, {1, 0, '\\'},
		},
	}
	crouchAttackFrames = [][]cell{
		{
			{-1, -1, 'o'}, {0, -1, 'O'}, {1, -1, 'o'},
			{-1, 0, '/'}, {0, 0, '='}, {1, 0, '\\'},
		},
		{
			{-1, -1, 'o'}, {0, -1, 'O'}, {1, -1, 'o'},
			{-1, 0, '/'}, {0, 0, '='}, {1, 0, '>'}, {2, 0, '>'},
		},
		{
			{-1, -1, 'o'}, {0, -1, 'O'}, {1, -1, 'o'},
			{-1, 0, '/'}, {0, 0, '='}, {1, 0, '\\'},
		},
	}
	jumpAttackFrames = [][]cell{
		{
			{-1, -3, '\\'}, {0, -3, 'O'}, {1, -3, '/'},
			{-1, -2, '-'}, {0, -2, '|'}, {1, -2, '-'},
			{-1, -1, '/'}, {1, -1, '\\'},
		},
		{
			{-1, -3, '\\'}, {0, -3, 'O'}, {1, -3, '/'},
			{-1, -2, '-'}, {0, -2, '|'}, {1, -2, '>'}, {2, -2, '>'},
			{-1, -1, '/'}, {1, -1, '\\'},
		},
		{
			{-1, -3, '\\'}, {0, -3, 'O'}, {1, -3, '/'},
			{-1, -2, '-'}, {0, -2, '|'}, {1, -2, '-'},
			{-1, -1, '/'}, {1, -1, '\\'},
		},
	}
	defendFrames = [][]cell{
		{
			{0, -2, 'O'},
			{-1, -1, '['}, {0, -1, '|'}, {1, -1, '-'},
			{-1, 0, '/'}, {1, 0, '\\'},
		},
		{
			{0, -2, 'O'},
			{-1, -1, '['}, {0, -1, '|'}, {1, -1, ']'},
			{-1, 0, '/'}, {1, 0, '\\'},
		},
	}
)

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

// --- Systems: rendering -------------------------------------------------

func renderFigures(c *app.Ctx) {
	fs := app.GetResource[Fighters](c)
	clock := app.GetResource[LocalClock](c)
	posMap := generic.NewMap1[tui.Position](c.World())
	glyphMap := generic.NewMap1[tui.Glyph](c.World())

	for _, f := range fs.Items {
		if f == nil {
			continue
		}
		var frame int
		switch {
		case f.Action == ActionAttack:
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
		case f.Posture == PostureJump, f.Posture == PostureCrouch:
			frame = 0
		default:
			frame = int((clock.Tick - f.AnimTick) / idleHoldTicks)
		}
		if frame < 0 {
			frame = 0
		}

		hit := f.HitCooldown > 0
		st := styleForFighter(f, hit)
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
	mp := app.GetResource[Match](c)

	drawText(s, 1, 0, hudStyle, " SPLITI STICK-FIGHT — vs AI ")

	for _, f := range fs.Items {
		if f == nil {
			continue
		}
		var label string
		var x int
		if f.Owner == humanID {
			label = " YOU "
			x = 1
		} else {
			label = "  AI "
			x = boardW - (len(label) + maxHealth + 2)
		}
		drawText(s, x, 1, styleForFighter(f, false), label)
		x += len(label)
		for h := 0; h < maxHealth; h++ {
			ch := '#'
			st := hpFull
			if h >= f.Health {
				ch = '.'
				st = hpEmpty
			}
			s.SetContent(x+h, 1, ch, nil, st)
		}
	}

	if app.GetState[GameMode](c).Get() == GameOver {
		var msg string
		switch {
		case mp.Drawn:
			msg = "  DOUBLE KO  "
		case mp.Winner == humanID:
			msg = "  YOU WIN  "
		default:
			msg = "  AI WINS  "
		}
		hint := "  press 'r' to restart, 'q' to quit  "
		w, _ := s.Size()
		drawText(s, (w-len(msg))/2, hudRows+boardH/2-1, overStyle, msg)
		drawText(s, (w-len(hint))/2, hudRows+boardH/2, overStyle, hint)
	}
}

func drawText(s tcell.Screen, x, y int, style tcell.Style, text string) {
	for i, r := range text {
		s.SetContent(x+i, y, r, nil, style)
	}
}

// --- Main ---------------------------------------------------------------

func main() {
	a := app.New()
	a.AddPlugins(defaultplugins.Plugins{
		Time: splititime.Plugin{
			FixedTimestep:   gotime.Second / 60,
			TargetFrameRate: 60,
		},
	})

	app.InsertResource(a, &Fighters{})
	app.InsertResource(a, &Match{})
	app.InsertResource(a, &LocalClock{})
	app.InsertResource(a, &AIBrain{})
	app.InitState(a, Playing)

	app.OnEnter(a, Playing, setupMatch)
	app.OnExit(a, Playing, teardownMatch)

	a.AddSystems(schedule.Update, handleLocalQuit)
	a.AddSystems(schedule.FixedUpdate, app.Chain(
		app.System(tickClock).Label("clock"),
		app.System(handleHumanInput).Label("human"),
		app.System(aiInput).Label("ai"),
		app.System(stepPhysics).Label("physics"),
		app.System(resolveCombat).Label("combat"),
		app.System(checkGameOver).Label("gameover"),
		app.System(renderFigures).Label("draw"),
	))
	tui.AddOverlay(a, renderHUD)

	a.Run()
}
