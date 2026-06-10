package main

import (
	"fmt"
	"image"
	"image/color"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/examples/radiosim/sim"
	"github.com/hvuhsg/spliti/plugin/render3d"
)

// UI caches the last-rendered status panel key so it is only re-rasterized when a
// setting changes.
type UI struct{ lastKey string }

const (
	uiW = 250
	uiH = 150
)

// updateUI draws the status-and-controls panel in the top-right corner: the
// active band, engine, weather, and wall material, plus a one-line key legend.
func updateUI(c *app.Ctx, scene *Scene, eng *Engine) {
	ui := app.GetResource[UI](c)
	if ui == nil {
		return
	}
	band := fmt.Sprintf("%.3g GHz", scene.Tx.FreqHz/1e9)
	engName := "image (exact)"
	if _, ok := eng.Engine.(sim.SBREngine); ok {
		engName = fmt.Sprintf("SBR x%d", scene.Cfg.NumRays)
	}
	rain := "clear"
	if scene.Sim.Weather.RainRateMMH > 0 {
		rain = fmt.Sprintf("rain %.0f mm/h", scene.Sim.Weather.RainRateMMH)
	}
	mat := scene.Sim.Mats.Get(scene.WallMat).Name

	key := band + "|" + engName + "|" + rain + "|" + mat
	winW, _ := render3d.WindowSize(c)
	if key != ui.lastKey {
		if err := render3d.LoadPanel(c, "ui", buildUIImage(band, engName, rain, mat)); err == nil {
			ui.lastKey = key
		}
	}
	render3d.DrawPanel(c, "ui", winW-uiW-12, 12, uiW, uiH)
}

// buildUIImage rasterizes the status/legend card.
func buildUIImage(band, engName, rain, mat string) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, uiW, uiH))
	fillRect(img, 0, 0, uiW, uiH, color.RGBA{12, 16, 24, 205})
	fillRect(img, 0, 0, uiW, 3, color.RGBA{255, 180, 80, 255})

	white := color.RGBA{235, 240, 245, 255}
	dim := color.RGBA{150, 165, 180, 255}
	accent := color.RGBA{255, 200, 120, 255}

	drawText(img, 12, 22, "SIMULATION", accent)
	drawText(img, 12, 42, "band "+band, white)
	drawText(img, 12, 58, "eng  "+engName, white)
	drawText(img, 12, 74, "air  "+rain, white)
	drawText(img, 12, 90, "wall "+mat, white)

	drawText(img, 12, 116, "1/2/3 band  T rain  M wall", dim)
	drawText(img, 12, 132, "G engine  H/R/space  drag rx", dim)
	return img
}
