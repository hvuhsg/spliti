package srcmodel

import (
	"fmt"
	"go/token"
	"path"
	"reflect"
	"strconv"

	"github.com/dave/dst"
	"github.com/mlange-42/arche/ecs"
)

// This file converts component values between live reflect form and the keyed
// composite literals of scene.Set lines:
//
//	scene.Set(c, crate1, components.Health{Max: 50, Current: 50})
//
// Encoding emits exported, non-zero fields only (zero fields are implied), so
// the source stays as terse as a hand-written override. Supported field kinds:
// bool, integers, floats, string, nested structs of the same kinds (the nested
// literal is qualified by its package's import, which is added to the file
// when missing), slices/arrays of those, and — through SetComponentValueRefs —
// entity references, written as the referenced spawn's variable name. Anything
// else (maps, pointers, channels) cannot be expressed in scene source; a
// non-zero unexpressible field is an error so the caller fails loudly instead
// of silently dropping data.

// EntityResolver maps a live entity handle to its scene instance name so an
// entity-reference field can be encoded as that spawn's variable.
type EntityResolver func(e ecs.Entity) (instance string, ok bool)

// VarLookup resolves a spawn variable name back to its live entity when a
// scene.Set literal holding entity references is decoded.
type VarLookup func(varName string) (ecs.Entity, bool)

var entityRefType = reflect.TypeFor[ecs.Entity]()

const ecsImportPath = "github.com/mlange-42/arche/ecs"

// encCtx carries encoding state: which live entities resolve to which spawn
// variable names, and the symbolic-layer table when the scene has one.
type encCtx struct {
	entityVar map[ecs.Entity]string
	layers    *Layers
}

// SetComponentValue encodes v (a struct) into a composite literal of the given
// written type name and upserts the instance's scene.Set line, adding imports
// nested literal types require. Entity-reference fields are rejected; use
// SetComponentValueRefs when the component may hold them.
func (s *Scene) SetComponentValue(instance, typeName string, v reflect.Value) error {
	return s.SetComponentValueRefs(instance, typeName, v, nil)
}

// SetComponentValueRefs is SetComponentValue with entity-reference support:
// every non-zero ecs.Entity in v is resolved to an instance name, that spawn
// is promoted to bind a variable when needed, and the scene.Set line is placed
// (or moved) after every spawn it references so the file still compiles.
func (s *Scene) SetComponentValueRefs(instance, typeName string, v reflect.Value, refs EntityResolver) error {
	ctx := &encCtx{entityVar: map[ecs.Entity]string{}, layers: s.layers}
	var refInstances []string
	for _, e := range entityRefs(v) {
		if _, done := ctx.entityVar[e]; done {
			continue
		}
		if refs == nil {
			return fmt.Errorf("srcmodel: %s: entity reference is not expressible here", typeName)
		}
		ref, ok := refs(e)
		if !ok {
			return fmt.Errorf("srcmodel: %s: entity reference does not point at a named instance", typeName)
		}
		varName, err := s.EnsureVar(ref)
		if err != nil {
			return err
		}
		ctx.entityVar[e] = varName
		refInstances = append(refInstances, ref)
	}
	lit, imports, err := litForValue(typeName, v, ctx)
	if err != nil {
		return err
	}
	for _, imp := range imports {
		ensureImport(s.file.file, imp)
	}
	return s.setComponentRefs(instance, lit, refInstances)
}

// entityRefs collects every non-zero entity handle reachable in v.
func entityRefs(v reflect.Value) []ecs.Entity {
	var out []ecs.Entity
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		if v.Type() == entityRefType {
			if !v.IsZero() {
				out = append(out, v.Interface().(ecs.Entity))
			}
			return
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		}
	}
	walk(v)
	return out
}

// LitForValue builds the keyed composite literal for struct value v, plus the
// import paths any nested literal types need. Entity references are rejected
// (there is no scene context to resolve them against).
func LitForValue(typeName string, v reflect.Value) (*dst.CompositeLit, []string, error) {
	return litForValue(typeName, v, nil)
}

func litForValue(typeName string, v reflect.Value, ctx *encCtx) (*dst.CompositeLit, []string, error) {
	typeExpr := nameExpr(typeName)
	if typeExpr == nil {
		return nil, nil, fmt.Errorf("srcmodel: invalid type name %q", typeName)
	}
	if v.Kind() != reflect.Struct {
		return nil, nil, fmt.Errorf("srcmodel: %s: component must be a struct", typeName)
	}
	var imports []string
	lit := &dst.CompositeLit{Type: typeExpr}
	rt := v.Type()
	// The composite literal names its own type (typeName), so the file must
	// import that type's package — even when no field needs an import. Nested
	// structs reach here too, so this covers every literal type in the tree.
	if pp := rt.PkgPath(); pp != "" {
		imports = append(imports, pp)
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		fv := v.Field(i)
		if !f.IsExported() || fv.IsZero() {
			continue
		}
		expr, imps, err := fieldExpr(f, fv, ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("srcmodel: %s.%s: %w", typeName, f.Name, err)
		}
		imports = append(imports, imps...)
		kv := &dst.KeyValueExpr{Key: dst.NewIdent(f.Name), Value: expr}
		lit.Elts = append(lit.Elts, kv)
	}
	return lit, imports, nil
}

// fieldExpr encodes one struct field value, honoring the `spliti` layer tags:
// with a symbolic-layer table installed, a tagged unsigned field is written as
// the game's named layer constants when its bits have names.
func fieldExpr(f reflect.StructField, v reflect.Value, ctx *encCtx) (dst.Expr, []string, error) {
	if tag := layerFieldTag(f.Tag.Get("spliti")); tag != "" && ctx != nil && ctx.layers != nil {
		switch v.Kind() {
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if expr, ok := ctx.layers.symbolicExpr(v.Uint(), tag == "layer"); ok {
				return expr, []string{ctx.layers.ImportPath}, nil
			}
		}
	}
	return valueExpr(v, ctx)
}

// valueExpr encodes one field value.
func valueExpr(v reflect.Value, ctx *encCtx) (dst.Expr, []string, error) {
	if v.Type() == entityRefType {
		if v.IsZero() {
			return &dst.CompositeLit{Type: &dst.SelectorExpr{
				X: dst.NewIdent("ecs"), Sel: dst.NewIdent("Entity"),
			}}, []string{ecsImportPath}, nil
		}
		if ctx != nil {
			if name, ok := ctx.entityVar[v.Interface().(ecs.Entity)]; ok {
				return dst.NewIdent(name), nil, nil
			}
		}
		return nil, nil, fmt.Errorf("entity reference is not expressible in scene source")
	}
	switch v.Kind() {
	case reflect.Bool:
		return dst.NewIdent(strconv.FormatBool(v.Bool())), nil, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return intExpr(v.Int()), nil, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &dst.BasicLit{Kind: token.INT, Value: strconv.FormatUint(v.Uint(), 10)}, nil, nil
	case reflect.Float32:
		return floatExpr(float32(v.Float())), nil, nil
	case reflect.Float64:
		return float64Expr(v.Float()), nil, nil
	case reflect.String:
		return &dst.BasicLit{Kind: token.STRING, Value: strconv.Quote(v.String())}, nil, nil
	case reflect.Struct:
		rt := v.Type()
		if rt.PkgPath() == "" || rt.Name() == "" {
			return nil, nil, fmt.Errorf("anonymous struct fields are not expressible in scene source")
		}
		qual := path.Base(rt.PkgPath())
		lit, imports, err := litForValue(qual+"."+rt.Name(), v, ctx)
		if err != nil {
			return nil, nil, err
		}
		// litForValue already includes rt's own package import.
		return lit, imports, nil
	case reflect.Slice, reflect.Array:
		eltType, imports, err := typeExprFor(v.Type().Elem())
		if err != nil {
			return nil, nil, err
		}
		at := &dst.ArrayType{Elt: eltType}
		if v.Kind() == reflect.Array {
			at.Len = &dst.BasicLit{Kind: token.INT, Value: strconv.Itoa(v.Type().Len())}
		}
		lit := &dst.CompositeLit{Type: at}
		for i := 0; i < v.Len(); i++ {
			expr, imps, err := valueExpr(v.Index(i), ctx)
			if err != nil {
				return nil, nil, fmt.Errorf("element %d: %w", i, err)
			}
			// Element literals repeat the slice's element type — elide it,
			// matching gofmt -s canonical form.
			if cl, ok := expr.(*dst.CompositeLit); ok {
				cl.Type = nil
			}
			imports = append(imports, imps...)
			lit.Elts = append(lit.Elts, expr)
		}
		return lit, imports, nil
	default:
		return nil, nil, fmt.Errorf("field kind %s is not expressible in scene source", v.Kind())
	}
}

// typeExprFor renders a reflect type as a source type expression (used for
// slice element types), plus the imports it needs.
func typeExprFor(t reflect.Type) (dst.Expr, []string, error) {
	if t.PkgPath() != "" {
		return &dst.SelectorExpr{
			X: dst.NewIdent(path.Base(t.PkgPath())), Sel: dst.NewIdent(t.Name()),
		}, []string{t.PkgPath()}, nil
	}
	switch t.Kind() {
	case reflect.Slice:
		elt, imports, err := typeExprFor(t.Elem())
		if err != nil {
			return nil, nil, err
		}
		return &dst.ArrayType{Elt: elt}, imports, nil
	case reflect.Array:
		elt, imports, err := typeExprFor(t.Elem())
		if err != nil {
			return nil, nil, err
		}
		return &dst.ArrayType{
			Len: &dst.BasicLit{Kind: token.INT, Value: strconv.Itoa(t.Len())},
			Elt: elt,
		}, imports, nil
	}
	if t.Name() != "" {
		return dst.NewIdent(t.Name()), nil, nil
	}
	return nil, nil, fmt.Errorf("type %s is not expressible in scene source", t)
}

func intExpr(n int64) dst.Expr {
	if n < 0 {
		return &dst.UnaryExpr{Op: token.SUB, X: &dst.BasicLit{
			Kind: token.INT, Value: strconv.FormatInt(-n, 10)}}
	}
	return &dst.BasicLit{Kind: token.INT, Value: strconv.FormatInt(n, 10)}
}

func float64Expr(v float64) dst.Expr {
	if v == 0 {
		v = 0 // fold negative zero
	}
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	kind := token.FLOAT
	if isIntLit(s) {
		kind = token.INT
	}
	lit := &dst.BasicLit{Kind: kind, Value: s}
	if neg {
		return &dst.UnaryExpr{Op: token.SUB, X: lit}
	}
	return lit
}

// ApplyLit decodes a keyed composite literal into v (a settable struct value).
// Fields absent from the literal are zeroed — a scene.Set line carries the
// whole component value, so absence means "zero", matching LitForValue.
// Entity references fail to decode; use ApplyLitRefs when the literal may hold
// them.
func ApplyLit(lit *dst.CompositeLit, v reflect.Value) error {
	return ApplyLitRefs(lit, v, nil)
}

// ApplyLitRefs is ApplyLit with entity-reference support: identifier values in
// entity-typed fields are resolved to live entities through look.
func ApplyLitRefs(lit *dst.CompositeLit, v reflect.Value, look VarLookup) error {
	return ApplyLitLayers(lit, v, look, nil)
}

// ApplyLitLayers is ApplyLitRefs with symbolic-layer support: named layer
// constants (and `|` chains of them) in unsigned fields resolve through the
// layers table.
func ApplyLitLayers(lit *dst.CompositeLit, v reflect.Value, look VarLookup, layers *Layers) error {
	if v.Kind() != reflect.Struct || !v.CanSet() {
		return fmt.Errorf("srcmodel: ApplyLit target must be a settable struct")
	}
	v.SetZero()
	for _, elt := range lit.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			return fmt.Errorf("srcmodel: positional composite literal is not editable")
		}
		key, ok := kv.Key.(*dst.Ident)
		if !ok {
			return fmt.Errorf("srcmodel: non-identifier literal key")
		}
		fv := v.FieldByName(key.Name)
		if !fv.IsValid() || !fv.CanSet() {
			return fmt.Errorf("srcmodel: unknown field %q in literal", key.Name)
		}
		if err := applyExpr(kv.Value, fv, look, layers); err != nil {
			return fmt.Errorf("srcmodel: field %s: %w", key.Name, err)
		}
	}
	return nil
}

func applyExpr(e dst.Expr, v reflect.Value, look VarLookup, layers *Layers) error {
	if v.Type() == entityRefType {
		switch ex := e.(type) {
		case *dst.Ident:
			if look != nil {
				if ent, ok := look(ex.Name); ok {
					v.Set(reflect.ValueOf(ent))
					return nil
				}
			}
			return fmt.Errorf("unresolved entity reference %q", ex.Name)
		case *dst.CompositeLit:
			if len(ex.Elts) == 0 { // ecs.Entity{} — the zero handle
				v.SetZero()
				return nil
			}
		}
		return fmt.Errorf("expected entity reference")
	}
	switch v.Kind() {
	case reflect.Bool:
		id, ok := e.(*dst.Ident)
		if !ok || (id.Name != "true" && id.Name != "false") {
			return fmt.Errorf("expected bool literal")
		}
		v.SetBool(id.Name == "true")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f, ok := floatLit(e)
		if !ok {
			return fmt.Errorf("expected numeric literal")
		}
		v.SetInt(int64(f))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if f, ok := floatLit(e); ok && f >= 0 {
			v.SetUint(uint64(f))
			return nil
		}
		// Named layer constants (`game.LayerPlayer`, `a | b`) when a symbolic
		// table is installed.
		if n, ok := layers.resolveExpr(e); ok {
			v.SetUint(n)
			return nil
		}
		return fmt.Errorf("expected unsigned numeric literal")
	case reflect.Float32, reflect.Float64:
		f, ok := floatLit(e)
		if !ok {
			return fmt.Errorf("expected numeric literal")
		}
		v.SetFloat(float64(f))
	case reflect.String:
		s, ok := stringLit(e)
		if !ok {
			return fmt.Errorf("expected string literal")
		}
		v.SetString(s)
	case reflect.Struct:
		lit, ok := e.(*dst.CompositeLit)
		if !ok {
			return fmt.Errorf("expected composite literal")
		}
		return ApplyLitLayers(lit, v, look, layers)
	case reflect.Slice:
		lit, ok := e.(*dst.CompositeLit)
		if !ok {
			return fmt.Errorf("expected slice literal")
		}
		s := reflect.MakeSlice(v.Type(), len(lit.Elts), len(lit.Elts))
		for i, elt := range lit.Elts {
			if err := applyExpr(elt, s.Index(i), look, layers); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
		}
		v.Set(s)
	case reflect.Array:
		lit, ok := e.(*dst.CompositeLit)
		if !ok {
			return fmt.Errorf("expected array literal")
		}
		if len(lit.Elts) > v.Len() {
			return fmt.Errorf("array literal has %d elements, want at most %d", len(lit.Elts), v.Len())
		}
		v.SetZero()
		for i, elt := range lit.Elts {
			if err := applyExpr(elt, v.Index(i), look, layers); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
		}
	default:
		return fmt.Errorf("field kind %s is not decodable from scene source", v.Kind())
	}
	return nil
}

// compositeEditable reports whether the literal consists entirely of values
// the editor can decode and rewrite — the precondition for the editor to own
// the scene.Set line. Struct literals are keyed; slice/array literals are
// positional. Identifiers naming spawn variables (vars) are entity references
// and count as editable, as do named layer constants resolvable through the
// scene's layers table (nil disables them).
func compositeEditable(lit *dst.CompositeLit, vars map[string]bool, layers *Layers) bool {
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*dst.KeyValueExpr); ok {
			if _, ok := kv.Key.(*dst.Ident); !ok {
				return false
			}
			if !literalExpr(kv.Value, vars, layers) {
				return false
			}
			continue
		}
		if !literalExpr(elt, vars, layers) {
			return false
		}
	}
	return true
}

func literalExpr(e dst.Expr, vars map[string]bool, layers *Layers) bool {
	switch v := e.(type) {
	case *dst.BasicLit:
		return true
	case *dst.Ident:
		return v.Name == "true" || v.Name == "false" || vars[v.Name]
	case *dst.UnaryExpr:
		if v.Op != token.SUB {
			return false
		}
		_, ok := v.X.(*dst.BasicLit)
		return ok
	case *dst.SelectorExpr, *dst.BinaryExpr:
		_, ok := layers.resolveExpr(e)
		return ok
	case *dst.CompositeLit:
		// Acceptable literal types: a named type, a slice/array type, or none
		// (elided element type inside a slice literal).
		if v.Type != nil {
			if _, isArr := v.Type.(*dst.ArrayType); !isArr && exprName(v.Type) == "" {
				return false
			}
		}
		return compositeEditable(v, vars, layers)
	}
	return false
}

// pruneUnusedImports drops import specs whose package qualifier is no longer
// mentioned anywhere in the file — the inverse of ensureImport, run on Save so
// removing the last scene.Set line that needed an import cannot leave the file
// uncompilable (unused imports are a Go compile error). Blank/dot imports and
// paths whose final element is not a valid identifier (the qualifier cannot be
// derived syntactically) are always kept.
func pruneUnusedImports(f *dst.File) {
	used := map[string]bool{}
	for _, decl := range f.Decls {
		if gd, ok := decl.(*dst.GenDecl); ok && gd.Tok == token.IMPORT {
			continue
		}
		dst.Inspect(decl, func(n dst.Node) bool {
			if id, ok := n.(*dst.Ident); ok {
				used[id.Name] = true
			}
			return true
		})
	}
	var decls []dst.Decl
	for _, decl := range f.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			decls = append(decls, decl)
			continue
		}
		var specs []dst.Spec
		for _, spec := range gd.Specs {
			is, ok := spec.(*dst.ImportSpec)
			if !ok {
				specs = append(specs, spec)
				continue
			}
			qual := ""
			if is.Name != nil {
				if is.Name.Name == "_" || is.Name.Name == "." {
					specs = append(specs, spec) // side-effect/dot import: keep
					continue
				}
				qual = is.Name.Name
			} else if p, err := strconv.Unquote(is.Path.Value); err == nil {
				qual = path.Base(p)
			}
			if !validIdent(qual) || used[qual] {
				specs = append(specs, spec)
			}
		}
		if len(specs) > 0 {
			gd.Specs = specs
			decls = append(decls, gd)
		}
	}
	f.Decls = decls
}

// ensureImport adds the import path to the file when missing. Imports are
// referenced by their canonical (last path element) qualifier, matching how
// scene files import engine packages.
func ensureImport(f *dst.File, importPath string) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gd.Specs {
			if is, ok := spec.(*dst.ImportSpec); ok {
				if p, err := strconv.Unquote(is.Path.Value); err == nil && p == importPath {
					return
				}
			}
		}
	}
	spec := &dst.ImportSpec{Path: &dst.BasicLit{
		Kind: token.STRING, Value: strconv.Quote(importPath)}}
	for _, decl := range f.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		gd.Specs = append(gd.Specs, spec)
		return
	}
	f.Decls = append([]dst.Decl{&dst.GenDecl{Tok: token.IMPORT, Specs: []dst.Spec{spec}}}, f.Decls...)
}
