package srcmodel

import (
	"fmt"
	"go/token"
	"path"
	"reflect"
	"strconv"

	"github.com/dave/dst"
)

// This file converts component values between live reflect form and the keyed
// composite literals of scene.Set lines:
//
//	scene.Set(c, crate1, components.Health{Max: 50, Current: 50})
//
// Encoding emits exported, non-zero fields only (zero fields are implied), so
// the source stays as terse as a hand-written override. Supported field kinds:
// bool, integers, floats, string, and nested structs of the same kinds (the
// nested literal is qualified by its package's import, which is added to the
// file when missing). Anything else — entity handles, slices, maps — cannot be
// expressed in scene source; a non-zero unexpressible field is an error so the
// caller fails loudly instead of silently dropping data.

// SetComponentValue encodes v (a struct) into a composite literal of the given
// written type name and upserts the instance's scene.Set line, adding imports
// nested literal types require.
func (s *Scene) SetComponentValue(instance, typeName string, v reflect.Value) error {
	lit, imports, err := LitForValue(typeName, v)
	if err != nil {
		return err
	}
	for _, imp := range imports {
		ensureImport(s.file.file, imp)
	}
	return s.SetComponent(instance, lit)
}

// LitForValue builds the keyed composite literal for struct value v, plus the
// import paths any nested literal types need.
func LitForValue(typeName string, v reflect.Value) (*dst.CompositeLit, []string, error) {
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
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		fv := v.Field(i)
		if !f.IsExported() || fv.IsZero() {
			continue
		}
		expr, imps, err := valueExpr(fv)
		if err != nil {
			return nil, nil, fmt.Errorf("srcmodel: %s.%s: %w", typeName, f.Name, err)
		}
		imports = append(imports, imps...)
		kv := &dst.KeyValueExpr{Key: dst.NewIdent(f.Name), Value: expr}
		lit.Elts = append(lit.Elts, kv)
	}
	return lit, imports, nil
}

// valueExpr encodes one field value.
func valueExpr(v reflect.Value) (dst.Expr, []string, error) {
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
		lit, imports, err := LitForValue(qual+"."+rt.Name(), v)
		if err != nil {
			return nil, nil, err
		}
		return lit, append(imports, rt.PkgPath()), nil
	default:
		return nil, nil, fmt.Errorf("field kind %s is not expressible in scene source", v.Kind())
	}
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
func ApplyLit(lit *dst.CompositeLit, v reflect.Value) error {
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
		if err := applyExpr(kv.Value, fv); err != nil {
			return fmt.Errorf("srcmodel: field %s: %w", key.Name, err)
		}
	}
	return nil
}

func applyExpr(e dst.Expr, v reflect.Value) error {
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
		f, ok := floatLit(e)
		if !ok || f < 0 {
			return fmt.Errorf("expected unsigned numeric literal")
		}
		v.SetUint(uint64(f))
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
		return ApplyLit(lit, v)
	default:
		return fmt.Errorf("field kind %s is not decodable from scene source", v.Kind())
	}
	return nil
}

// compositeEditable reports whether the literal is fully keyed with literal
// (or nested keyed-literal) values — the precondition for the editor to own
// and rewrite the scene.Set line.
func compositeEditable(lit *dst.CompositeLit) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		if !ok {
			return false
		}
		if _, ok := kv.Key.(*dst.Ident); !ok {
			return false
		}
		if !literalExpr(kv.Value) {
			return false
		}
	}
	return true
}

func literalExpr(e dst.Expr) bool {
	switch v := e.(type) {
	case *dst.BasicLit:
		return true
	case *dst.Ident:
		return v.Name == "true" || v.Name == "false"
	case *dst.UnaryExpr:
		if v.Op != token.SUB {
			return false
		}
		_, ok := v.X.(*dst.BasicLit)
		return ok
	case *dst.CompositeLit:
		if exprName(v.Type) == "" {
			return false
		}
		return compositeEditable(v)
	}
	return false
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
