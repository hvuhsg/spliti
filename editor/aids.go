package editor

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// drawGrid pushes the ground-plane reference grid into the scene's line pass
// each frame: 1-unit cells over a 20x20 area with emphasized world axes.
func drawGrid(c *app.Ctx) {
	const half = float32(10)
	minor := m.Vec4{X: 0.32, Y: 0.34, Z: 0.38, W: 0.6}
	xAxis := m.Vec4{X: 0.85, Y: 0.3, Z: 0.3, W: 0.9}
	zAxis := m.Vec4{X: 0.35, Y: 0.5, Z: 0.9, W: 0.9}
	for i := -int(half); i <= int(half); i++ {
		f := float32(i)
		cz := minor
		if i == 0 {
			cz = zAxis
		}
		render3d.Line(c, m.Vec3{X: f, Z: -half}, m.Vec3{X: f, Z: half}, cz)
		cx := minor
		if i == 0 {
			cx = xAxis
		}
		render3d.Line(c, m.Vec3{X: -half, Z: f}, m.Vec3{X: half, Z: f}, cx)
	}
}
