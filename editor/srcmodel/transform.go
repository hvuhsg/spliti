package srcmodel

import (
	"fmt"
	"go/token"
	"math"
	"strconv"

	"github.com/dave/dst"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// parseTransformChain recognizes a literal render3d.XForm() builder chain:
//
//	render3d.XForm().At(-1.4, 0.6, 0).EulerDeg(0, 30, 0).Scaled(2, 2, 2)
//
// Any other expression — a different base, a repeated or unknown method, a
// non-literal argument — yields a non-editable Transform so the editor leaves
// the expression untouched.
func parseTransformChain(e dst.Expr) *Transform {
	t := &Transform{}
	cur := e
	for {
		call, ok := cur.(*dst.CallExpr)
		if !ok {
			return &Transform{}
		}
		switch fun := call.Fun.(type) {
		case *dst.SelectorExpr:
			if id, ok := fun.X.(*dst.Ident); ok && id.Name == xformPkg {
				// Chain base: must be exactly render3d.XForm().
				if fun.Sel.Name != "XForm" || len(call.Args) != 0 {
					return &Transform{}
				}
				t.Editable = true
				return t
			}
			// Method link: parse this call, then descend into the receiver.
			switch fun.Sel.Name {
			case "At":
				if t.At != nil {
					return &Transform{}
				}
				t.At = floatArgs3(call.Args)
				if t.At == nil {
					return &Transform{}
				}
			case "EulerDeg":
				if t.EulerDeg != nil || t.Rot != nil {
					return &Transform{}
				}
				t.EulerDeg = floatArgs3(call.Args)
				if t.EulerDeg == nil {
					return &Transform{}
				}
			case "Scaled":
				if t.Scaled != nil {
					return &Transform{}
				}
				t.Scaled = floatArgs3(call.Args)
				if t.Scaled == nil {
					return &Transform{}
				}
			case "Rot":
				if t.Rot != nil || t.EulerDeg != nil {
					return &Transform{}
				}
				t.Rot = floatArgs4(call.Args)
				if t.Rot == nil {
					return &Transform{}
				}
			default:
				return &Transform{}
			}
			cur = fun.X
		default:
			return &Transform{}
		}
	}
}

// Value reconstructs the render3d.Transform3D the chain evaluates to.
func (t *Transform) Value() render3d.Transform3D {
	out := render3d.XForm()
	if t.At != nil {
		out = out.At(t.At[0], t.At[1], t.At[2])
	}
	if t.EulerDeg != nil {
		out = out.EulerDeg(t.EulerDeg[0], t.EulerDeg[1], t.EulerDeg[2])
	}
	if t.Rot != nil {
		out = out.Rot(t.Rot[0], t.Rot[1], t.Rot[2], t.Rot[3])
	}
	if t.Scaled != nil {
		out = out.Scaled(t.Scaled[0], t.Scaled[1], t.Scaled[2])
	}
	return out
}

// FromTransform3D converts a live transform into its canonical chain form:
// .At when translated, .EulerDeg in degrees when the rotation survives a round
// trip through Euler angles (rounded to 0.001°), .Rot otherwise, .Scaled when
// scaled. The zero/default parts of the transform emit no chain call at all.
// Translation and scale are rounded to 0.001 — gizmo drags otherwise leave
// float32 noise like -5.53e-08 in source, and a scene file is for humans too.
func FromTransform3D(v render3d.Transform3D) Transform {
	t := Transform{Editable: true}
	v.Translation = m.Vec3{X: roundMilli(v.Translation.X), Y: roundMilli(v.Translation.Y), Z: roundMilli(v.Translation.Z)}
	v.Scale = m.Vec3{X: roundMilli(v.Scale.X), Y: roundMilli(v.Scale.Y), Z: roundMilli(v.Scale.Z)}
	if v.Translation != (m.Vec3{}) {
		t.At = &[3]float32{v.Translation.X, v.Translation.Y, v.Translation.Z}
	}
	rot := v.Rotation
	if rot == (m.Quat{}) {
		rot = m.IdentityQuat()
	}
	rot = rot.Normalize()
	if rot != m.IdentityQuat() {
		p, y, r := rot.ToEuler()
		deg := [3]float32{roundMilli(radToDeg(p)), roundMilli(radToDeg(y)), roundMilli(radToDeg(r))}
		back := m.FromEuler(m.DegToRad(deg[0]), m.DegToRad(deg[1]), m.DegToRad(deg[2]))
		if quatClose(back, rot) {
			if deg != ([3]float32{}) {
				t.EulerDeg = &deg
			}
		} else {
			t.Rot = &[4]float32{rot.X, rot.Y, rot.Z, rot.W}
		}
	}
	if v.Scale != (m.Vec3{}) && v.Scale != (m.Vec3{X: 1, Y: 1, Z: 1}) {
		t.Scaled = &[3]float32{v.Scale.X, v.Scale.Y, v.Scale.Z}
	}
	return t
}

// SetTransform replaces the transform chain on the instance's spawn line with
// the canonical chain for t. The existing chain must be editable (all-literal)
// and the prefab call must already carry a transform argument.
func (s *Scene) SetTransform(instance string, t Transform) error {
	sp := s.Spawn(instance)
	if sp == nil {
		return fmt.Errorf("srcmodel: scene %q has no instance %q", s.Name, instance)
	}
	if sp.Transform == nil {
		return fmt.Errorf("srcmodel: instance %q: prefab call has no transform argument", instance)
	}
	if !sp.Transform.Editable {
		return fmt.Errorf("srcmodel: instance %q: transform expression is not a literal chain", instance)
	}
	old := sp.prefabCall.Args[1]
	repl := chainExpr(t)
	// Keep the statement's surrounding formatting: the new expression inherits
	// the old argument's decorations (leading/trailing comments stay put).
	repl.Decs.NodeDecs = *old.Decorations()
	sp.prefabCall.Args[1] = repl
	nt := t
	sp.Transform = &nt
	return nil
}

// AddSpawn appends a new spawn line to the scene function:
//
//	_ = scene.Spawn(c, "instance", prefab(c, <chain>))
//
// prefab is written verbatim (e.g. "entities.SpawnCrate"); instance must not
// already exist in the scene.
func (s *Scene) AddSpawn(instance, prefab string, t Transform) (*Spawn, error) {
	if s.Spawn(instance) != nil {
		return nil, fmt.Errorf("srcmodel: scene %q already has instance %q", s.Name, instance)
	}
	if s.fn.Body == nil {
		return nil, fmt.Errorf("srcmodel: scene %q has no function body", s.Name)
	}
	prefabFun := nameExpr(prefab)
	if prefabFun == nil {
		return nil, fmt.Errorf("srcmodel: invalid prefab name %q", prefab)
	}
	prefabCall := &dst.CallExpr{
		Fun:  prefabFun,
		Args: []dst.Expr{dst.NewIdent(s.ctx), chainExpr(t)},
	}
	spawnCall := &dst.CallExpr{
		Fun: &dst.SelectorExpr{X: dst.NewIdent(scenePkg), Sel: dst.NewIdent("Spawn")},
		Args: []dst.Expr{
			dst.NewIdent(s.ctx),
			&dst.BasicLit{Kind: token.STRING, Value: strconv.Quote(instance)},
			prefabCall,
		},
	}
	stmt := &dst.AssignStmt{
		Lhs: []dst.Expr{dst.NewIdent("_")},
		Tok: token.ASSIGN,
		Rhs: []dst.Expr{spawnCall},
	}
	stmt.Decs.Before = dst.NewLine
	stmt.Decs.After = dst.NewLine
	s.fn.Body.List = append(s.fn.Body.List, stmt)
	s.rescan()
	return s.Spawn(instance), nil
}

// chainExpr builds the canonical render3d.XForm()... expression for t.
func chainExpr(t Transform) *dst.CallExpr {
	cur := &dst.CallExpr{
		Fun: &dst.SelectorExpr{X: dst.NewIdent(xformPkg), Sel: dst.NewIdent("XForm")},
	}
	link := func(name string, vals []float32) {
		args := make([]dst.Expr, len(vals))
		for i, v := range vals {
			args[i] = floatExpr(v)
		}
		cur = &dst.CallExpr{
			Fun:  &dst.SelectorExpr{X: cur, Sel: dst.NewIdent(name)},
			Args: args,
		}
	}
	if t.At != nil {
		link("At", t.At[:])
	}
	if t.EulerDeg != nil {
		link("EulerDeg", t.EulerDeg[:])
	}
	if t.Rot != nil {
		link("Rot", t.Rot[:])
	}
	if t.Scaled != nil {
		link("Scaled", t.Scaled[:])
	}
	return cur
}

// nameExpr parses "Name" or "pkg.Name" into an expression; nil if malformed.
func nameExpr(name string) dst.Expr {
	for i, r := range name {
		if r == '.' {
			pkg, sel := name[:i], name[i+1:]
			if !validIdent(pkg) || !validIdent(sel) {
				return nil
			}
			return &dst.SelectorExpr{X: dst.NewIdent(pkg), Sel: dst.NewIdent(sel)}
		}
	}
	if !validIdent(name) {
		return nil
	}
	return dst.NewIdent(name)
}

func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		alpha := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		digit := r >= '0' && r <= '9'
		if !alpha && (!digit || i == 0) {
			return false
		}
	}
	return true
}

// floatExpr renders v with the fewest digits that round-trip as float32,
// negative values as a unary minus.
func floatExpr(v float32) dst.Expr {
	if v == 0 {
		v = 0 // fold negative zero (atan2 artifacts) into "0"
	}
	if v < 0 {
		return &dst.UnaryExpr{Op: token.SUB, X: floatLitExpr(-v)}
	}
	return floatLitExpr(v)
}

func floatLitExpr(v float32) *dst.BasicLit {
	s := strconv.FormatFloat(float64(v), 'g', -1, 32)
	kind := token.FLOAT
	if isIntLit(s) {
		kind = token.INT
	}
	return &dst.BasicLit{Kind: kind, Value: s}
}

func isIntLit(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// floatArgs3/floatArgs4 read all-literal call arguments; nil when any
// argument is not a (possibly negated) numeric literal.
func floatArgs3(args []dst.Expr) *[3]float32 {
	if len(args) != 3 {
		return nil
	}
	var out [3]float32
	for i, a := range args {
		v, ok := floatLit(a)
		if !ok {
			return nil
		}
		out[i] = v
	}
	return &out
}

func floatArgs4(args []dst.Expr) *[4]float32 {
	if len(args) != 4 {
		return nil
	}
	var out [4]float32
	for i, a := range args {
		v, ok := floatLit(a)
		if !ok {
			return nil
		}
		out[i] = v
	}
	return &out
}

func floatLit(e dst.Expr) (float32, bool) {
	neg := false
	if u, ok := e.(*dst.UnaryExpr); ok {
		if u.Op != token.SUB {
			return 0, false
		}
		neg = true
		e = u.X
	}
	lit, ok := e.(*dst.BasicLit)
	if !ok || (lit.Kind != token.FLOAT && lit.Kind != token.INT) {
		return 0, false
	}
	v, err := strconv.ParseFloat(lit.Value, 32)
	if err != nil {
		return 0, false
	}
	if neg {
		v = -v
	}
	return float32(v), true
}

func radToDeg(r float32) float32 { return r * (180 / math.Pi) }

func roundMilli(v float32) float32 {
	return float32(math.Round(float64(v)*1000) / 1000)
}

// quatClose treats q and r as the same rotation when they differ by less than
// ~0.16° (|dot| = cos(θ/2); q and -q are the same rotation). Loose enough to
// absorb float32 noise from the Euler round trip, tight enough that a real
// gimbal-lock information loss falls back to the .Rot form.
func quatClose(q, r m.Quat) bool {
	dot := float64(q.X*r.X + q.Y*r.Y + q.Z*r.Z + q.W*r.W)
	return math.Abs(dot) > 1-1e-6
}
