package srcmodel

import (
	"strconv"

	"github.com/dave/dst"
	"go/token"
)

// Layers is the symbolic-layer table for scene encoding: the game's named
// collision layer bits (from the //spliti:layers const block) plus where they
// live. With a table installed (Scene.SetLayers), component fields tagged
// `spliti:"layer"` / `spliti:"layers"` are written to scene source as the
// named constants — `game.LayerPlayer`, `game.LayerDefault | game.LayerEnemy`
// — and such expressions are decoded and counted as editable when parsing.
type Layers struct {
	Pkg        string   // package qualifier as written in scene source, e.g. "game"
	ImportPath string   // the package's import path, e.g. "mygame/game"
	Names      []string // index = bit position; "" for unnamed bits
}

// bitValue resolves a constant name to its bit value.
func (l *Layers) bitValue(name string) (uint64, bool) {
	for bit, n := range l.Names {
		if n != "" && n == name {
			return 1 << bit, true
		}
	}
	return 0, false
}

// resolveExpr evaluates a layer-bit expression: an integer literal, a
// `pkg.LayerName` selector, or a `|` chain of those.
func (l *Layers) resolveExpr(e dst.Expr) (uint64, bool) {
	if l == nil {
		return 0, false
	}
	switch ex := e.(type) {
	case *dst.BasicLit:
		if ex.Kind != token.INT {
			return 0, false
		}
		n, err := strconv.ParseUint(ex.Value, 0, 64)
		return n, err == nil
	case *dst.SelectorExpr:
		pkg, ok := ex.X.(*dst.Ident)
		if !ok || pkg.Name != l.Pkg {
			return 0, false
		}
		return l.bitValue(ex.Sel.Name)
	case *dst.BinaryExpr:
		if ex.Op != token.OR {
			return 0, false
		}
		a, ok := l.resolveExpr(ex.X)
		if !ok {
			return 0, false
		}
		b, ok := l.resolveExpr(ex.Y)
		if !ok {
			return 0, false
		}
		return a | b, true
	case *dst.ParenExpr:
		return l.resolveExpr(ex.X)
	}
	return 0, false
}

// symbolicExpr renders v as named layer constants. For a single-layer field
// (single) v must be exactly one named bit; for a mask field every named bit
// becomes a selector and any unnamed remainder one trailing integer literal.
// Returns ok=false when nothing can be named (the caller falls back to a
// plain numeral).
func (l *Layers) symbolicExpr(v uint64, single bool) (dst.Expr, bool) {
	if l == nil || v == 0 {
		return nil, false
	}
	sel := func(name string) dst.Expr {
		return &dst.SelectorExpr{X: dst.NewIdent(l.Pkg), Sel: dst.NewIdent(name)}
	}
	if single {
		for bit, name := range l.Names {
			if name != "" && v == 1<<bit {
				return sel(name), true
			}
		}
		return nil, false
	}
	var expr dst.Expr
	rest := v
	for bit, name := range l.Names {
		b := uint64(1) << bit
		if name == "" || rest&b == 0 {
			continue
		}
		rest &^= b
		s := sel(name)
		if expr == nil {
			expr = s
		} else {
			expr = &dst.BinaryExpr{X: expr, Op: token.OR, Y: s}
		}
	}
	if expr == nil {
		return nil, false // no named bits: plain numeral reads better than 0|n
	}
	if rest != 0 {
		expr = &dst.BinaryExpr{X: expr, Op: token.OR, Y: &dst.BasicLit{
			Kind: token.INT, Value: strconv.FormatUint(rest, 10),
		}}
	}
	return expr, true
}

// layerFieldTag reports the `spliti` layer tag on a struct field: "layer" for
// a single named bit, "layers" for a mask, "" otherwise.
func layerFieldTag(tag string) string {
	switch tag {
	case "layer", "layers":
		return tag
	}
	return ""
}

// SetLayers installs (or, with nil, removes) the symbolic-layer table and
// re-recognizes the scene's lines under it, so existing scene.Set literals
// using named constants become editable.
func (s *Scene) SetLayers(l *Layers) {
	s.layers = l
	s.rescan()
}
