package main

import (
	"fmt"
	"math"
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/canvas"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/mlange-42/arche/ecs"
)

const statusRows = 1 // terminal rows reserved at the bottom for the HUD bar

// render is the per-frame draw, registered as a tui overlay. It runs after the
// tui plugin clears the screen and before the single present, so it draws the
// whole scene with Screen.SetContent (via the canvas blit) and must not call
// Screen.Show itself.
func render(c *app.Ctx) {
	s := tui.Screen(c)
	if s == nil {
		return
	}
	w, h := tui.Size(c)
	if w < 20 || h < 8 {
		drawText(s, 0, 0, tcell.StyleDefault.Foreground(tcell.ColorWhite), "terminal too small")
		return
	}

	g := app.GetResource[Game](c)
	p := app.GetResource[Player](c)

	viewRows := h - statusRows
	viewW := w
	if g.Canvas == nil {
		g.Canvas = canvas.New(viewW, viewRows)
	} else {
		g.Canvas.Resize(viewW, viewRows)
	}
	if len(g.Depth) != viewW {
		g.Depth = make([]float64, viewW)
	}
	cv := g.Canvas

	drawBackground(cv)
	castWalls(cv, g, p)
	castSprites(c, cv, g, p)
	applyHurtTint(cv, p)
	cv.Blit(s, 0, 0)

	drawCrosshair(s, viewW, viewRows)
	drawWeapon(s, viewW, viewRows, p)
	drawHUD(s, w, h, p)
	drawBanner(c, s, w, h)
}

// --- Background (floor / ceiling) ---------------------------------------

func drawBackground(cv *canvas.Canvas) {
	horizon := cv.PH / 2
	for py := 0; py < cv.PH; py++ {
		var col canvas.RGB
		if py < horizon {
			// Ceiling: dark slate at the top, lightening toward the horizon.
			t := float64(py) / float64(maxInt(horizon, 1))
			col = canvas.RGB{R: uint8(18 + 26*t), G: uint8(18 + 26*t), B: uint8(30 + 40*t)}
		} else {
			// Floor: brighter close to the camera (bottom of the screen).
			t := float64(py-horizon) / float64(maxInt(cv.PH-horizon, 1))
			b := 0.30 + 0.70*t
			col = canvas.RGB{R: uint8(95 * b), G: uint8(72 * b), B: uint8(46 * b)}
		}
		for x := 0; x < cv.CW; x++ {
			cv.Set(x, py, col)
		}
	}
}

// --- Walls (DDA raycast) -------------------------------------------------

func castWalls(cv *canvas.Canvas, g *Game, p *Player) {
	for x := 0; x < cv.CW; x++ {
		cameraX := 2*float64(x)/float64(cv.CW) - 1
		rayX := p.DirX + p.PlaneX*cameraX
		rayY := p.DirY + p.PlaneY*cameraX

		mapX, mapY := int(p.X), int(p.Y)
		deltaX := math.Abs(1 / rayX)
		deltaY := math.Abs(1 / rayY)

		var stepX, stepY int
		var sideDistX, sideDistY float64
		if rayX < 0 {
			stepX = -1
			sideDistX = (p.X - float64(mapX)) * deltaX
		} else {
			stepX = 1
			sideDistX = (float64(mapX) + 1 - p.X) * deltaX
		}
		if rayY < 0 {
			stepY = -1
			sideDistY = (p.Y - float64(mapY)) * deltaY
		} else {
			stepY = 1
			sideDistY = (float64(mapY) + 1 - p.Y) * deltaY
		}

		side := 0
		var wallID uint8
		for {
			if sideDistX < sideDistY {
				sideDistX += deltaX
				mapX += stepX
				side = 0
			} else {
				sideDistY += deltaY
				mapY += stepY
				side = 1
			}
			if wallID = g.Map.At(mapX, mapY); wallID != 0 {
				break
			}
		}

		var perp float64
		if side == 0 {
			perp = (float64(mapX) - p.X + (1-float64(stepX))/2) / rayX
		} else {
			perp = (float64(mapY) - p.Y + (1-float64(stepY))/2) / rayY
		}
		if perp < 1e-4 {
			perp = 1e-4
		}
		g.Depth[x] = perp

		lineH := int(float64(cv.PH) / perp)
		start := cv.PH/2 - lineH/2
		end := cv.PH/2 + lineH/2

		f := fog(perp)
		if side == 1 {
			f *= 0.68 // fake directional lighting: y-side walls are dimmer
		}
		// Seam shading at cell boundaries for a faint "brick" feel.
		var wallX float64
		if side == 0 {
			wallX = p.Y + perp*rayY
		} else {
			wallX = p.X + perp*rayX
		}
		wallX -= math.Floor(wallX)
		if wallX < 0.04 || wallX > 0.96 {
			f *= 0.6
		}

		cv.VLine(x, start, end, shade(wallColor(wallID), f))
	}
}

// wallColor maps a wall id to its base color.
func wallColor(id uint8) canvas.RGB {
	switch id {
	case 2:
		return canvas.RGB{R: 175, G: 60, B: 48} // red brick
	case 3:
		return canvas.RGB{R: 70, G: 95, B: 180} // blue metal
	default:
		return canvas.RGB{R: 135, G: 135, B: 140} // stone
	}
}

// --- Sprites (billboarded enemies & pickups) -----------------------------

type visMob struct {
	x, y  float64
	kind  uint8
	enemy bool
	d2    float64
}

func castSprites(c *app.Ctx, cv *canvas.Canvas, g *Game, p *Player) {
	var mobs []visMob
	app.Query2[Enemy, Vec2](c, func(_ ecs.Entity, en *Enemy, v *Vec2) {
		dx, dy := v.X-p.X, v.Y-p.Y
		mobs = append(mobs, visMob{x: v.X, y: v.Y, kind: en.Kind, enemy: true, d2: dx*dx + dy*dy})
	})
	app.Query2[Pickup, Vec2](c, func(_ ecs.Entity, pk *Pickup, v *Vec2) {
		dx, dy := v.X-p.X, v.Y-p.Y
		mobs = append(mobs, visMob{x: v.X, y: v.Y, kind: pk.Kind, enemy: false, d2: dx*dx + dy*dy})
	})
	// Painter's order: farthest first so nearer sprites overwrite.
	sort.Slice(mobs, func(i, j int) bool { return mobs[i].d2 > mobs[j].d2 })

	invDet := 1.0 / (p.PlaneX*p.DirY - p.DirX*p.PlaneY)
	for _, m := range mobs {
		spx, spy := m.x-p.X, m.y-p.Y
		tx := invDet * (p.DirY*spx - p.DirX*spy)
		ty := invDet * (-p.PlaneY*spx + p.PlaneX*spy) // depth
		if ty <= 0.05 {
			continue
		}

		screenX := int(float64(cv.CW) / 2 * (1 + tx/ty))
		lineH := int(float64(cv.PH) / ty)

		var vScale, wScale float64
		if m.enemy {
			vScale, wScale = 0.95, 0.55
		} else {
			vScale, wScale = 0.42, 0.5
		}
		spriteH := int(float64(lineH) * vScale)
		spriteW := int(float64(lineH) * wScale)
		if spriteH < 1 || spriteW < 1 {
			continue
		}
		floorY := cv.PH/2 + lineH/2
		startY := floorY - spriteH
		startX := screenX - spriteW/2

		ffog := fog(ty)
		for sx := 0; sx < spriteW; sx++ {
			col := startX + sx
			if col < 0 || col >= cv.CW {
				continue
			}
			if ty >= g.Depth[col] { // occluded by a nearer wall
				continue
			}
			u := (float64(sx) + 0.5) / float64(spriteW)
			for sy := 0; sy < spriteH; sy++ {
				row := startY + sy
				if row < 0 || row >= cv.PH {
					continue
				}
				vv := (float64(sy) + 0.5) / float64(spriteH)
				if cc, ok := sampleSprite(m.kind, m.enemy, u, vv); ok {
					cv.Set(col, row, shade(cc, ffog))
				}
			}
		}
	}
}

// sampleSprite returns the color of a billboard at texture coords (u,v) in
// [0,1], and false where the sprite is transparent. v=0 is the top.
func sampleSprite(kind uint8, enemy bool, u, v float64) (canvas.RGB, bool) {
	cx := u - 0.5
	if enemy { // imp: dark-red humanoid with glowing eyes
		switch {
		case v < 0.28: // head
			if math.Abs(cx) >= 0.16 {
				return canvas.RGB{}, false
			}
			if v > 0.10 && v < 0.19 && (math.Abs(cx+0.07) < 0.04 || math.Abs(cx-0.07) < 0.04) {
				return canvas.RGB{R: 255, G: 230, B: 40}, true // eyes
			}
			return canvas.RGB{R: 120, G: 32, B: 26}, true
		case v < 0.66: // torso + arms
			if math.Abs(cx) >= 0.30 {
				return canvas.RGB{}, false
			}
			return shade(canvas.RGB{R: 175, G: 48, B: 36}, 1.0-math.Abs(cx)*0.8), true
		default: // two legs
			ac := math.Abs(cx)
			if ac > 0.06 && ac < 0.26 {
				return canvas.RGB{R: 110, G: 35, B: 30}, true
			}
			return canvas.RGB{}, false
		}
	}

	switch kind {
	case pickHealth: // green cross
		if math.Abs(cx) < 0.5 && math.Abs(v-0.5) < 0.5 &&
			(math.Abs(cx) < 0.14 || math.Abs(v-0.5) < 0.14) {
			return canvas.RGB{R: 40, G: 220, B: 70}, true
		}
	case pickAmmo: // banded box
		if u > 0.28 && u < 0.72 && v > 0.32 && v < 0.92 {
			if int(v*8)%2 == 0 {
				return canvas.RGB{R: 215, G: 175, B: 45}, true
			}
			return canvas.RGB{R: 150, G: 110, B: 30}, true
		}
	}
	return canvas.RGB{}, false
}

// --- Effects -------------------------------------------------------------

func applyHurtTint(cv *canvas.Canvas, p *Player) {
	if p.HurtTicks <= 0 {
		return
	}
	a := 0.45 * float64(p.HurtTicks) / float64(hurtTicks)
	for i := range cv.Pix {
		px := &cv.Pix[i]
		px.R = lerpU8(px.R, 200, a)
		px.G = lerpU8(px.G, 0, a)
		px.B = lerpU8(px.B, 0, a)
	}
}

// --- HUD / overlay (drawn in cells, on top of the blitted canvas) --------

func drawCrosshair(s tcell.Screen, viewW, viewRows int) {
	st := tcell.StyleDefault.Foreground(tcell.NewRGBColor(230, 230, 230))
	s.SetContent(viewW/2, viewRows/2, '+', nil, st)
}

func drawWeapon(s tcell.Screen, viewW, viewRows int, p *Player) {
	gun := []string{
		"  ███  ",
		"  ███  ",
		" █████ ",
		"███████",
	}
	gw := len([]rune(gun[0]))
	baseY := viewRows - len(gun)
	if p.MuzzleTicks > 0 {
		baseY-- // recoil kick
	}
	startX := viewW/2 - gw/2
	metal := tcell.StyleDefault.Foreground(tcell.NewRGBColor(90, 90, 100))
	for dy, row := range gun {
		x := startX
		for _, r := range row {
			if r != ' ' {
				s.SetContent(x, baseY+dy, r, nil, metal)
			}
			x++
		}
	}
	if p.MuzzleTicks > 0 {
		flash := tcell.StyleDefault.Foreground(tcell.NewRGBColor(255, 230, 120))
		fy := baseY - 1
		for dx := -1; dx <= 1; dx++ {
			s.SetContent(viewW/2+dx, fy, '▓', nil, flash)
		}
		s.SetContent(viewW/2, fy-1, '▒', nil, flash)
	}
}

func drawHUD(s tcell.Screen, w, h int, p *Player) {
	y := h - 1
	barStyle := tcell.StyleDefault.
		Background(tcell.NewRGBColor(25, 25, 30)).
		Foreground(tcell.NewRGBColor(220, 220, 220))
	for x := 0; x < w; x++ {
		s.SetContent(x, y, ' ', nil, barStyle)
	}
	hpStyle := barStyle.Foreground(hpColor(p.HP))
	drawText(s, 1, y, hpStyle, fmt.Sprintf("HP %3d", p.HP))
	drawText(s, 10, y, barStyle.Foreground(tcell.NewRGBColor(230, 200, 80)),
		fmt.Sprintf("AMMO %3d", p.Ammo))
	drawText(s, 22, y, barStyle, fmt.Sprintf("KILLS %d/%d", p.Kills, len(enemySpawns)))
}

func drawBanner(c *app.Ctx, s tcell.Screen, w, h int) {
	state := app.GetState[GameMode](c).Get()
	g := app.GetResource[Game](c)

	// Transient pickup message, just above the HUD bar.
	if g.MsgTicks > 0 && g.Message != "" {
		st := tcell.StyleDefault.Foreground(tcell.NewRGBColor(255, 255, 180))
		drawText(s, w/2-len([]rune(g.Message))/2, h-2, st, g.Message)
	}

	if state == Playing {
		return
	}
	var title, sub string
	var col tcell.Color
	if state == Dead {
		title, sub = "YOU DIED", "press R to restart  -  Esc to quit"
		col = tcell.NewRGBColor(220, 50, 50)
	} else {
		title, sub = "LEVEL CLEAR", "press R to play again  -  Esc to quit"
		col = tcell.NewRGBColor(80, 220, 90)
	}
	cy := h / 2
	titleStyle := tcell.StyleDefault.Foreground(col).Bold(true)
	subStyle := tcell.StyleDefault.Foreground(tcell.NewRGBColor(230, 230, 230))
	drawText(s, w/2-len([]rune(title))/2, cy, titleStyle, title)
	drawText(s, w/2-len([]rune(sub))/2, cy+2, subStyle, sub)
}

func drawText(s tcell.Screen, x, y int, style tcell.Style, text string) {
	for i, r := range []rune(text) {
		s.SetContent(x+i, y, r, nil, style)
	}
}

// --- Small helpers -------------------------------------------------------

// fog returns a brightness multiplier that falls off with distance.
func fog(dist float64) float64 {
	f := 1.0 - dist*0.045
	if f < 0.15 {
		f = 0.15
	}
	return f
}

func shade(col canvas.RGB, f float64) canvas.RGB {
	if f < 0 {
		f = 0
	}
	return canvas.RGB{
		R: clampU8(float64(col.R) * f),
		G: clampU8(float64(col.G) * f),
		B: clampU8(float64(col.B) * f),
	}
}

func clampU8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func lerpU8(from uint8, to uint8, a float64) uint8 {
	return clampU8(float64(from)*(1-a) + float64(to)*a)
}

func hpColor(hp int) tcell.Color {
	if hp <= 25 {
		return tcell.NewRGBColor(230, 60, 60)
	}
	if hp <= 50 {
		return tcell.NewRGBColor(230, 200, 80)
	}
	return tcell.NewRGBColor(90, 220, 110)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
