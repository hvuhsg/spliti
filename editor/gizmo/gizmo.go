package gizmo

import (
	"fmt"
	"math"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// Op selects which transform the gizmo edits.
type Op int

const (
	Translate Op = iota
	Rotate
	Scale
)

// Mode selects the axis frame. Scale always operates in Local space
// (world-space scaling of a rotated object would shear).
type Mode int

const (
	World Mode = iota
	Local
)

// Frame is the per-call viewport state. Positions are in ImGui display space
// (the same pixels as the viewport image rect). MouseDown must be polled from
// the windowing layer (render3d.MouseButtonDown), NOT from ImGui events —
// ImGui eats mouse releases, and a drag that never sees the release sticks.
type Frame struct {
	View, Proj m.Mat4
	RectMin    m.Vec2
	RectSize   m.Vec2
	MousePos   m.Vec2
	MouseDown  bool    // left mouse button held
	Snap       float32 // 0 = off; world units (translate), degrees (rotate), factor (scale)
}

// part identifies one interactive piece of the gizmo. Axis parts double as
// axis indices.
type part int

const (
	partNone   part = -1
	partAxisX  part = 0
	partAxisY  part = 1
	partAxisZ  part = 2
	partPlaneX part = 3 // plane ⊥ X (drag in YZ), etc.
	partPlaneY part = 4
	partPlaneZ part = 5
	partCenter part = 6 // screen-plane move / uniform scale / view-axis rotate
)

const (
	gizmoPx       = 96  // on-screen half-extent of the gizmo
	pickPx        = 8   // hit half-width for lines and circles
	centerPx      = 7   // radius of the center disc
	planeMin      = 0.3 // plane quad inner corner, in screenFactor units
	planeMax      = 0.55
	ringScale     = 1.0  // rotation circle radius, in screenFactor units
	viewRingScale = 1.18 // outer view-axis rotation ring
	minAxisPx     = 14   // hide an axis when it projects shorter than this
	circleSegs    = 64
)

// Gizmo draws and drives one transform gizmo. Zero value is ready to use; keep
// it across frames (it holds the drag state machine).
type Gizmo struct {
	using    bool
	over     bool
	started  bool
	finished bool

	op         Op
	active     part
	axes       [3]m.Vec3 // unit gizmo axes at drag start, world space
	startModel m.Mat4
	startHit   m.Vec3 // first ray/plane hit of the drag
	planeP0    m.Vec3 // drag plane
	planeN     m.Vec3
	startMouse m.Vec2
	lastScale  float32 // factor applied by the live scale drag, for the label
	prevDown   bool
}

// Using reports whether a drag is in progress.
func (g *Gizmo) Using() bool { return g.using }

// Over reports whether the cursor is on a gizmo part (or a drag is active).
// Callers must skip click-picking while it is true.
func (g *Gizmo) Over() bool { return g.over }

// Started reports that a drag began during the last Manipulate call.
func (g *Gizmo) Started() bool { return g.started }

// Finished reports that a drag ended during the last Manipulate call; the
// model held StartModel before it. Pair the two for one undo entry.
func (g *Gizmo) Finished() bool { return g.finished }

// StartModel returns the model matrix captured when the current (or just
// finished) drag began.
func (g *Gizmo) StartModel() m.Mat4 { return g.startModel }

// Manipulate draws the gizmo for model into dl and applies any mouse
// interaction. It returns true when it modified model this frame.
func (g *Gizmo) Manipulate(dl *imgui.DrawList, f Frame, op Op, mode Mode, model *m.Mat4) bool {
	g.started, g.finished = false, false
	pressed := f.MouseDown && !g.prevDown
	g.prevDown = f.MouseDown

	cam, ok := newCamera(f.View, f.Proj, f.RectMin, f.RectSize)
	if !ok {
		g.abort()
		return false
	}
	origin := matTranslation(*model)
	originPx, visible := cam.worldToScreen(origin)
	if !visible {
		g.abort()
		return false
	}
	sf := cam.screenFactor(origin, gizmoPx)
	axes := gizmoAxes(*model, mode, op)

	changed := false
	switch {
	case g.using && (op != g.op || !f.MouseDown):
		// Release — or the operation switched mid-drag, which also ends it.
		g.using = false
		g.finished = true
		g.active = partNone
	case g.using:
		changed = g.applyDrag(cam, f, model)
	default:
		hot := partNone
		if mouseInRect(f) && (!f.MouseDown || pressed) {
			hot = hitTest(cam, f, op, origin, originPx, axes, sf)
		}
		if pressed && hot != partNone {
			g.begin(cam, f, op, hot, origin, axes, *model)
		} else {
			g.active = hot
		}
	}

	g.draw(dl, cam, f, op, origin, originPx, axes, sf)
	g.over = g.using || g.active != partNone
	return changed
}

// abort drops any drag without emitting Finished (the camera became
// degenerate or the target left the frustum).
func (g *Gizmo) abort() {
	g.using = false
	g.active = partNone
	g.over = false
}

// gizmoAxes returns the three unit axes the gizmo aligns to: world axes, or
// the model's rotation columns in Local mode. Scale is always local.
func gizmoAxes(model m.Mat4, mode Mode, op Op) [3]m.Vec3 {
	if mode == World && op != Scale {
		return [3]m.Vec3{{X: 1}, {Y: 1}, {Z: 1}}
	}
	var axes [3]m.Vec3
	for i := 0; i < 3; i++ {
		col := matCol3(model, i)
		if col.LengthSq() < 1e-12 {
			col = m.Vec3{X: b2f(i == 0), Y: b2f(i == 1), Z: b2f(i == 2)}
		}
		axes[i] = col.Normalize()
	}
	return axes
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

func mouseInRect(f Frame) bool {
	return f.MousePos.X >= f.RectMin.X && f.MousePos.Y >= f.RectMin.Y &&
		f.MousePos.X <= f.RectMin.X+f.RectSize.X &&
		f.MousePos.Y <= f.RectMin.Y+f.RectSize.Y
}

// axisEnds returns the screen endpoints of axis i's handle and whether the
// axis is usable (projects long enough to grab).
func axisEnds(cam camera, origin m.Vec3, axis m.Vec3, sf float32) (a, b m.Vec2, ok bool) {
	a, okA := cam.worldToScreen(origin)
	b, okB := cam.worldToScreen(origin.Add(axis.Scale(sf)))
	if !okA || !okB || v2dist(a, b) < minAxisPx {
		return a, b, false
	}
	return a, b, true
}

// planeCorners returns the screen-space corners of the plane handle ⊥ axis i.
func planeCorners(cam camera, origin m.Vec3, axes [3]m.Vec3, i int, sf float32) ([4]m.Vec2, bool) {
	u, v := axes[(i+1)%3], axes[(i+2)%3]
	pts := [4]m.Vec3{
		origin.Add(u.Scale(planeMin * sf)).Add(v.Scale(planeMin * sf)),
		origin.Add(u.Scale(planeMax * sf)).Add(v.Scale(planeMin * sf)),
		origin.Add(u.Scale(planeMax * sf)).Add(v.Scale(planeMax * sf)),
		origin.Add(u.Scale(planeMin * sf)).Add(v.Scale(planeMax * sf)),
	}
	var out [4]m.Vec2
	for j, p := range pts {
		px, ok := cam.worldToScreen(p)
		if !ok {
			return out, false
		}
		out[j] = px
	}
	// Edge-on quads project to slivers: ungrabbable and just visual noise.
	if quadArea(out[0], out[1], out[2], out[3]) < 30 {
		return out, false
	}
	return out, true
}

// ringPoints projects the circle of radius r*sf around origin in the plane
// ⊥ axis into screen space.
func ringPoints(cam camera, origin, axis m.Vec3, r, sf float32) ([circleSegs]m.Vec2, bool) {
	var pts [circleSegs]m.Vec2
	u, v := planeBasis(axis)
	for i := 0; i < circleSegs; i++ {
		t := float64(i) / circleSegs * 2 * math.Pi
		p := origin.
			Add(u.Scale(r * sf * float32(math.Cos(t)))).
			Add(v.Scale(r * sf * float32(math.Sin(t))))
		px, ok := cam.worldToScreen(p)
		if !ok {
			return pts, false
		}
		pts[i] = px
	}
	return pts, true
}

// hitTest finds the hottest part under the mouse for the given operation.
// Closest-wins among parts within pick range; the center disc and planes get
// priority over axis lines at equal distance (they're smaller targets).
func hitTest(cam camera, f Frame, op Op, origin m.Vec3, originPx m.Vec2, axes [3]m.Vec3, sf float32) part {
	mouse := f.MousePos

	if op == Rotate {
		best, bestD := partNone, float32(pickPx)
		for i := 0; i < 3; i++ {
			pts, ok := ringPoints(cam, origin, axes[i], ringScale, sf)
			if !ok {
				continue
			}
			for j := range pts {
				d := distPointSegment(mouse, pts[j], pts[(j+1)%circleSegs])
				if d < bestD {
					best, bestD = part(i), d
				}
			}
		}
		// View-axis ring: a screen-space circle, cheap exact test.
		if d := float32(math.Abs(float64(v2dist(mouse, originPx) - viewRingScale*gizmoPx))); d < bestD {
			best = partCenter
		}
		return best
	}

	// Translate / Scale share the layout: center disc, plane quads, axis lines.
	if v2dist(mouse, originPx) <= centerPx+2 {
		return partCenter
	}
	if op == Translate {
		for i := 0; i < 3; i++ {
			if q, ok := planeCorners(cam, origin, axes, i, sf); ok &&
				pointInQuad(mouse, q[0], q[1], q[2], q[3]) {
				return partPlaneX + part(i)
			}
		}
	}
	best, bestD := partNone, float32(pickPx)
	for i := 0; i < 3; i++ {
		a, b, ok := axisEnds(cam, origin, axes[i], sf)
		if !ok {
			continue
		}
		if d := distPointSegment(mouse, a, b); d < bestD {
			best, bestD = part(i), d
		}
	}
	return best
}

// begin starts a drag on the given part: captures the model, picks the drag
// plane, and records the first hit so applyDrag works in deltas.
func (g *Gizmo) begin(cam camera, f Frame, op Op, p part, origin m.Vec3, axes [3]m.Vec3, model m.Mat4) {
	g.using = true
	g.started = true
	g.op = op
	g.active = p
	g.axes = axes
	g.startModel = model
	g.startMouse = f.MousePos
	g.lastScale = 1
	g.planeP0 = origin

	switch {
	case op == Rotate:
		axis := cam.fwd
		if p != partCenter {
			axis = axes[p]
		}
		g.planeN = axis
	case p == partCenter: // screen-plane move / uniform scale
		g.planeN = cam.fwd
	case p >= partPlaneX: // plane handle: drag on that plane
		g.planeN = axes[p-partPlaneX]
	default: // axis handle: camera-facing plane containing the axis
		g.planeN = axisDragPlane(origin, axes[p], cam.eye)
	}

	ro, rd := cam.mouseRay(f.MousePos)
	if hit, ok := intersectRayPlane(ro, rd, g.planeP0, g.planeN); ok {
		g.startHit = hit
	} else {
		g.startHit = origin
	}
}

// applyDrag recomputes model from the live mouse position, always relative to
// the state captured in begin (no per-frame error accumulation).
func (g *Gizmo) applyDrag(cam camera, f Frame, model *m.Mat4) bool {
	ro, rd := cam.mouseRay(f.MousePos)
	hit, ok := intersectRayPlane(ro, rd, g.planeP0, g.planeN)
	if !ok {
		return false
	}
	origin := matTranslation(g.startModel)
	out := g.startModel

	switch {
	case g.op == Translate:
		delta := hit.Sub(g.startHit)
		switch {
		case g.active == partCenter:
			u, v := planeBasis(g.planeN)
			delta = u.Scale(snapF(delta.Dot(u), f.Snap)).
				Add(v.Scale(snapF(delta.Dot(v), f.Snap)))
		case g.active >= partPlaneX:
			i := int(g.active - partPlaneX)
			u, v := g.axes[(i+1)%3], g.axes[(i+2)%3]
			delta = u.Scale(snapF(delta.Dot(u), f.Snap)).
				Add(v.Scale(snapF(delta.Dot(v), f.Snap)))
		default:
			axis := g.axes[g.active]
			delta = axis.Scale(snapF(delta.Dot(axis), f.Snap))
		}
		setMatTranslation(&out, origin.Add(delta))

	case g.op == Rotate:
		a := g.startHit.Sub(origin)
		b := hit.Sub(origin)
		if a.LengthSq() < 1e-9 || b.LengthSq() < 1e-9 {
			return false
		}
		angle := signedAngleOnPlane(a.Normalize(), b.Normalize(), g.planeN)
		angle = m.DegToRad(snapF(m.RadToDeg(angle), f.Snap))
		out = rotateLinearPart(g.startModel, g.planeN, angle)

	default: // Scale
		var factor float32
		if g.active == partCenter {
			startD := v2dist(g.startMouse, mustScreen(cam, origin))
			curD := v2dist(f.MousePos, mustScreen(cam, origin))
			if startD < 1 {
				return false
			}
			factor = curD / startD
		} else {
			axis := g.axes[g.active]
			start := g.startHit.Sub(origin).Dot(axis)
			cur := hit.Sub(origin).Dot(axis)
			if float32(math.Abs(float64(start))) < 1e-6 {
				return false
			}
			factor = cur / start
		}
		factor = snapF(factor, f.Snap)
		if factor < 0.001 {
			factor = 0.001
		}
		g.lastScale = factor
		if g.active == partCenter {
			for i := 0; i < 3; i++ {
				setMatCol3(&out, i, matCol3(g.startModel, i).Scale(factor))
			}
		} else {
			i := int(g.active)
			setMatCol3(&out, i, matCol3(g.startModel, i).Scale(factor))
		}
	}

	if out == *model {
		return false
	}
	*model = out
	return true
}

func mustScreen(cam camera, p m.Vec3) m.Vec2 {
	px, _ := cam.worldToScreen(p)
	return px
}

// --- drawing ---

func col32(r, g, b, a uint32) uint32 { return a<<24 | b<<16 | g<<8 | r }

var (
	colAxis = [3]uint32{
		col32(221, 50, 50, 255),
		col32(50, 200, 50, 255),
		col32(70, 110, 255, 255),
	}
	colPlane = [3]uint32{
		col32(221, 50, 50, 130),
		col32(50, 200, 50, 130),
		col32(70, 110, 255, 130),
	}
	colHot      = col32(255, 190, 35, 255)
	colHotFill  = col32(255, 190, 35, 150)
	colCenter   = col32(220, 220, 220, 200)
	colViewRing = col32(190, 190, 190, 170)
	colGuide    = col32(255, 190, 35, 110)
	colText     = col32(255, 255, 255, 255)
	colShadow   = col32(0, 0, 0, 220)
)

func px(v m.Vec2) imgui.Vec2 { return imgui.Vec2{X: v.X, Y: v.Y} }

func (g *Gizmo) hotIs(p part) bool { return g.active == p }

func (g *Gizmo) draw(dl *imgui.DrawList, cam camera, f Frame, op Op, origin m.Vec3, originPx m.Vec2, axes [3]m.Vec3, sf float32) {
	// While dragging, draw the captured axes so the gizmo doesn't swim under
	// the cursor as the model rotates/scales.
	if g.using {
		axes = g.axes
		origin = matTranslation(g.startModel)
		if op == Translate {
			// ...except translate, where the gizmo rides with the object.
			origin = g.dragOrigin(cam, f)
		}
		var ok bool
		originPx, ok = cam.worldToScreen(origin)
		if !ok {
			return
		}
	}

	if op == Rotate {
		for i := 0; i < 3; i++ {
			pts, ok := ringPoints(cam, origin, axes[i], ringScale, sf)
			if !ok {
				continue
			}
			col := colAxis[i]
			if g.hotIs(part(i)) {
				col = colHot
			}
			drawRing(dl, pts[:], col, 2)
		}
		col := colViewRing
		if g.hotIs(partCenter) {
			col = colHot
		}
		dl.AddCircleV(px(originPx), viewRingScale*gizmoPx, col, circleSegs, 2)
		if g.using {
			g.drawRotateFeedback(dl, cam, f, origin, originPx)
		}
		return
	}

	// Plane handles (translate only).
	if op == Translate {
		for i := 0; i < 3; i++ {
			q, ok := planeCorners(cam, origin, axes, i, sf)
			if !ok {
				continue
			}
			fill, line := colPlane[i], colAxis[i]
			if g.hotIs(partPlaneX + part(i)) {
				fill, line = colHotFill, colHot
			}
			dl.AddQuadFilled(px(q[0]), px(q[1]), px(q[2]), px(q[3]), fill)
			dl.AddQuadV(px(q[0]), px(q[1]), px(q[2]), px(q[3]), line, 1)
		}
	}

	// Axis handles with arrow (translate) or square (scale) tips.
	for i := 0; i < 3; i++ {
		a, b, ok := axisEnds(cam, origin, axes[i], sf)
		if !ok {
			continue
		}
		col := colAxis[i]
		if g.hotIs(part(i)) {
			col = colHot
		}
		dl.AddLineV(px(a), px(b), col, 3)
		dir := b.Sub(a)
		n := v2len(dir)
		if n < 1e-3 {
			continue
		}
		dir = dir.Scale(1 / n)
		perp := m.Vec2{X: -dir.Y, Y: dir.X}
		if op == Scale {
			c := px(b)
			h := float32(5)
			dl.AddQuadFilled(
				imgui.Vec2{X: c.X - perp.X*h - dir.X*h, Y: c.Y - perp.Y*h - dir.Y*h},
				imgui.Vec2{X: c.X + perp.X*h - dir.X*h, Y: c.Y + perp.Y*h - dir.Y*h},
				imgui.Vec2{X: c.X + perp.X*h + dir.X*h, Y: c.Y + perp.Y*h + dir.Y*h},
				imgui.Vec2{X: c.X - perp.X*h + dir.X*h, Y: c.Y - perp.Y*h + dir.Y*h},
				col)
		} else {
			tip := b.Add(dir.Scale(12))
			dl.AddTriangleFilled(px(tip),
				px(b.Add(perp.Scale(5))), px(b.Sub(perp.Scale(5))), col)
		}
	}

	// Center disc.
	col := colCenter
	if g.hotIs(partCenter) {
		col = colHot
	}
	dl.AddCircleFilledV(px(originPx), centerPx, col, 24)

	if g.using {
		g.drawDragFeedback(dl, cam, f, origin, originPx)
	}
}

// dragOrigin returns where the translated object currently sits (start origin
// plus the constrained delta), so the gizmo follows it during the drag.
func (g *Gizmo) dragOrigin(cam camera, f Frame) m.Vec3 {
	origin := matTranslation(g.startModel)
	ro, rd := cam.mouseRay(f.MousePos)
	hit, ok := intersectRayPlane(ro, rd, g.planeP0, g.planeN)
	if !ok {
		return origin
	}
	delta := hit.Sub(g.startHit)
	switch {
	case g.active == partCenter:
	case g.active >= partPlaneX:
	default:
		axis := g.axes[g.active]
		delta = axis.Scale(delta.Dot(axis))
	}
	return origin.Add(delta)
}

func drawRing(dl *imgui.DrawList, pts []m.Vec2, col uint32, thickness float32) {
	for i := range pts {
		dl.AddLineV(px(pts[i]), px(pts[(i+1)%len(pts)]), col, thickness)
	}
}

func (g *Gizmo) drawDragFeedback(dl *imgui.DrawList, cam camera, f Frame, origin m.Vec3, originPx m.Vec2) {
	startPx, ok := cam.worldToScreen(matTranslation(g.startModel))
	if !ok {
		return
	}
	var label string
	switch g.op {
	case Translate:
		d := origin.Sub(matTranslation(g.startModel))
		dl.AddLineV(px(startPx), px(originPx), colGuide, 1)
		label = fmt.Sprintf("Δ %.3f, %.3f, %.3f", d.X, d.Y, d.Z)
	case Scale:
		label = fmt.Sprintf("×%.3f", g.lastScale)
	}
	if label != "" {
		drawLabel(dl, f.MousePos, label)
	}
}

func (g *Gizmo) drawRotateFeedback(dl *imgui.DrawList, cam camera, f Frame, origin m.Vec3, originPx m.Vec2) {
	ro, rd := cam.mouseRay(f.MousePos)
	hit, ok := intersectRayPlane(ro, rd, g.planeP0, g.planeN)
	if !ok {
		return
	}
	a := g.startHit.Sub(origin)
	b := hit.Sub(origin)
	if a.LengthSq() < 1e-9 || b.LengthSq() < 1e-9 {
		return
	}
	angle := m.RadToDeg(signedAngleOnPlane(a.Normalize(), b.Normalize(), g.planeN))
	if sp, ok := cam.worldToScreen(g.startHit); ok {
		dl.AddLineV(px(originPx), px(sp), colGuide, 1)
	}
	if hp, ok := cam.worldToScreen(hit); ok {
		dl.AddLineV(px(originPx), px(hp), colHot, 1)
	}
	drawLabel(dl, f.MousePos, fmt.Sprintf("%.1f°", snapF(angle, f.Snap)))
}

func drawLabel(dl *imgui.DrawList, at m.Vec2, text string) {
	pos := imgui.Vec2{X: at.X + 14, Y: at.Y + 14}
	dl.AddTextVec2(imgui.Vec2{X: pos.X + 1, Y: pos.Y + 1}, colShadow, text)
	dl.AddTextVec2(pos, colText, text)
}
