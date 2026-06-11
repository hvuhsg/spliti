package render3d

import (
	"image/color"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/text"
)

// LoadTextPanel rasterizes s with the face and uploads it as the overlay
// panel ref — text on the GPU HUD in one call. Draw it each frame with
// DrawPanel (zero w/h uses the text's native pixel size):
//
//	face := text.Default(24)            // once, at startup
//	render3d.LoadTextPanel(c, "score", face, fmt.Sprintf("Score: %d", n), color.White)  // when n changes
//	render3d.DrawPanel(c, "score", 16, 16, 0, 0)  // every frame
//
// Reload only when the string changes; rasterizing and re-uploading every
// frame is wasteful (see Overlay2D).
func LoadTextPanel(c *app.Ctx, ref string, face *text.Face, s string, col color.Color) error {
	return LoadPanel(c, ref, face.Render(s, col))
}
