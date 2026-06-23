package systems

import (
	"fmt"
	"image"
	"image/color"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/text"
)

// HUD caches the font and the last stats string so the text panel is only
// re-rasterized when a value changes (rebaking every frame is wasteful).
type HUD struct {
	face      *text.Face
	loaded    bool
	lastStats string
}

// NewHUD builds the HUD resource; panels are loaded lazily on the first draw,
// once the renderer is live.
func NewHUD() *HUD { return &HUD{} }

// DrawHUD renders the crosshair, health bar, ammo/wave/kills readout, hit
// marker, damage flash, and the death banner. Registered via render3d.AddOverlay
// so it runs after the 3D pass every frame, in all states.
func DrawHUD(c *app.Ctx) {
	hud := app.GetResource[HUD](c)
	cb := app.GetResource[Combat](c)
	p := app.GetResource[Player](c)
	if hud == nil || cb == nil || p == nil {
		return
	}
	// DrawPanel positions in framebuffer pixels, so use Size (not the logical
	// WindowSize) or the HUD lands in a corner on Retina displays. Scale every
	// element by the device-pixel ratio so the HUD reads the same size at any DPI.
	w, h := render3d.Size(c)
	winW, _ := render3d.WindowSize(c)
	if w == 0 || h == 0 || winW == 0 {
		return
	}
	s := float32(w) / float32(winW) // 2 on a typical Retina display, else 1
	px := func(v float32) int { return int(v * s) }

	if !hud.loaded {
		hud.face = text.Default(float64(22 * s))
		loadStaticPanels(c, hud.face)
		hud.loaded = true
	}

	// First-person weapon viewmodel (a 2D sprite, so it never intercepts the
	// player's own hitscan rays). It bobs up with recoil and flashes when firing.
	gw, gh := px(300), px(240)
	kick := px(p.recoil / RecoilKick * 9)
	gx, gy := w/2-gw/2+px(50), h-gh+px(30)-kick
	render3d.DrawPanel(c, "weapon", gx, gy, gw, gh)
	if cb.MuzzleTime > 0 {
		render3d.DrawPanel(c, "muzzle", gx+gw/2-px(40), gy-px(30), px(64), px(64))
	}

	// Crosshair.
	cs := px(16)
	render3d.DrawPanel(c, "crosshair", w/2-cs/2, h/2-cs/2, cs, cs)

	// Health bar, bottom-left.
	bw, bh := px(280), px(18)
	bx, by := px(28), h-px(44)
	render3d.DrawPanel(c, "hud-dark", bx-px(3), by-px(3), bw+px(6), bh+px(6))
	render3d.DrawPanel(c, "hud-red", bx, by, bw, bh)
	frac := clampF(p.Health/PlayerMaxHP, 0, 1)
	render3d.DrawPanel(c, "hud-green", bx, by, int(frac*float32(bw)), bh)

	// Stats text, top-left (rebuilt only on change; the face is already DPI-sized).
	key := fmt.Sprintf("%d|%d|%d|%d", int(p.Health+0.5), cb.Ammo, cb.Wave, cb.Kills)
	if key != hud.lastStats {
		str := fmt.Sprintf("HP %d    AMMO %d    WAVE %d    KILLS %d",
			int(p.Health+0.5), cb.Ammo, cb.Wave, cb.Kills)
		if render3d.LoadTextPanel(c, "hud-stats", hud.face, str, color.White) == nil {
			hud.lastStats = key
		}
	}
	render3d.DrawPanel(c, "hud-stats", px(28), px(20), 0, 0)

	// Hit marker pip when a shot connected.
	if cb.HitMarkerTime > 0 {
		hm := px(18)
		render3d.DrawPanel(c, "hitmarker", w/2-hm/2, h/2-hm/2, hm, hm)
	}
	// Red flash while taking damage.
	if cb.HurtTime > 0 {
		render3d.DrawPanel(c, "hurt", 0, 0, w, h)
	}

	// Death banner.
	if app.GetState[GameMode](c).Get() == Dead {
		pw, ph := render3d.PanelSize(c, "banner")
		render3d.DrawPanel(c, "banner", w/2-pw/2, h/2-ph/2, pw, ph)
	}
}

// loadStaticPanels uploads the panels that never change: the crosshair, hit
// marker, the solid-color swatches the bars stretch, the hurt flash, and the
// death banner text.
func loadStaticPanels(c *app.Ctx, face *text.Face) {
	render3d.LoadPanel(c, "crosshair", crosshairImg())
	render3d.LoadPanel(c, "hitmarker", hitMarkerImg())
	render3d.LoadPanel(c, "weapon", weaponImg())
	render3d.LoadPanel(c, "muzzle", muzzleImg())
	render3d.LoadPanel(c, "hud-dark", solid(color.RGBA{18, 22, 30, 210}))
	render3d.LoadPanel(c, "hud-red", solid(color.RGBA{120, 32, 32, 230}))
	render3d.LoadPanel(c, "hud-green", solid(color.RGBA{70, 200, 95, 240}))
	render3d.LoadPanel(c, "hurt", solid(color.RGBA{200, 30, 30, 70}))
	render3d.LoadTextPanel(c, "banner", face, "GAME OVER  —  press R to restart",
		color.RGBA{255, 220, 220, 255})
}

// solid returns a 1x1 image of one color, stretched to any rect by DrawPanel.
func solid(col color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, col)
	return img
}

// crosshairImg draws a centered white plus with a small gap.
func crosshairImg() image.Image {
	const n = 16
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	col := color.RGBA{255, 255, 255, 230}
	for i := 0; i < n; i++ {
		if i < 6 || i > 9 { // leave a gap around the centre
			img.SetRGBA(i, n/2, col)
			img.SetRGBA(i, n/2-1, col)
			img.SetRGBA(n/2, i, col)
			img.SetRGBA(n/2-1, i, col)
		}
	}
	return img
}

// fillRect fills a rectangle in img, clipped to bounds.
func fillRect(img *image.RGBA, x, y, w, h int, col color.RGBA) {
	b := img.Bounds()
	for yy := y; yy < y+h; yy++ {
		if yy < b.Min.Y || yy >= b.Max.Y {
			continue
		}
		for xx := x; xx < x+w; xx++ {
			if xx >= b.Min.X && xx < b.Max.X {
				img.SetRGBA(xx, yy, col)
			}
		}
	}
}

// weaponImg draws a simple low-poly blaster viewmodel, seen from behind, rising
// from the bottom-centre of the screen.
func weaponImg() image.Image {
	const w, h = 260, 200
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	dark := color.RGBA{38, 42, 52, 255}
	mid := color.RGBA{60, 66, 80, 255}
	accent := color.RGBA{90, 160, 210, 255}
	// Barrel running up toward the screen centre.
	fillRect(img, w/2-16, 0, 32, h-70, mid)
	fillRect(img, w/2-8, 0, 16, h-70, dark)
	fillRect(img, w/2-18, 20, 36, 8, accent) // sight ring glow
	// Receiver body.
	fillRect(img, w/2-46, h-86, 92, 52, dark)
	fillRect(img, w/2-46, h-86, 92, 8, mid)
	// Grip angling to the lower-right (the hand side).
	fillRect(img, w/2+10, h-44, 30, 44, dark)
	return img
}

// muzzleImg draws a star-ish muzzle flash.
func muzzleImg() image.Image {
	const n = 56
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	core := color.RGBA{255, 240, 180, 255}
	glow := color.RGBA{255, 180, 60, 200}
	c := n / 2
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := x-c, y-c
			d := dx*dx + dy*dy
			switch {
			case d < 9*9:
				img.SetRGBA(x, y, core)
			case d < 16*16 && (abs(dx) < 4 || abs(dy) < 4 || abs(dx-dy) < 4 || abs(dx+dy) < 4):
				img.SetRGBA(x, y, glow)
			}
		}
	}
	return img
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// hitMarkerImg draws a small red X shown briefly on a connecting shot.
func hitMarkerImg() image.Image {
	const n = 18
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	col := color.RGBA{255, 70, 70, 240}
	for i := 0; i < n; i++ {
		img.SetRGBA(i, i, col)
		img.SetRGBA(n-1-i, i, col)
	}
	return img
}
