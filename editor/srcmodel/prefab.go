package srcmodel

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// sceneImportPath is the engine package that holds Spawn/Set/etc.
const sceneImportPath = "github.com/hvuhsg/spliti/scene"

// PrefabComponent is one extra component to attach in a generated prefab, with
// its written (package-qualified) type name and a struct value to encode.
type PrefabComponent struct {
	TypeName string // e.g. "components.Spinner"
	Value    reflect.Value
}

// RenderPrefab builds the Go source of a //spliti:entity prefab function in
// package `entities`: it creates a mesh entity from the spawn transform and
// scene.Set's each given component, returning the entity. Components whose value
// cannot be expressed as a scene literal (e.g. they hold entity references) are
// skipped and named in `skipped` rather than failing the whole prefab. funcName
// must be a valid exported identifier; mesh and material are the registered keys.
func RenderPrefab(funcName, mesh, material string, comps []PrefabComponent) (src []byte, skipped []string, err error) {
	skeleton := fmt.Sprintf(`package entities

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/mlange-42/arche/ecs"
)

// %s is a prefab extracted in the editor.
//
//spliti:entity
func %s(c *app.Ctx, t render3d.Transform3D) ecs.Entity {
	e := render3d.NewMesh(c, t, %s, %s)
	return e
}
`, funcName, funcName, strconv.Quote(mesh), strconv.Quote(material))

	f, err := decorator.ParseFile(token.NewFileSet(), "", skeleton, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("srcmodel: prefab skeleton: %w", err)
	}
	fn := prefabFuncDecl(f)
	if fn == nil {
		return nil, nil, fmt.Errorf("srcmodel: prefab skeleton lost its function")
	}

	var sets []dst.Stmt
	needScene := false
	for _, comp := range comps {
		lit, imports, lerr := LitForValue(comp.TypeName, comp.Value)
		if lerr != nil {
			skipped = append(skipped, comp.TypeName)
			continue
		}
		needScene = true
		for _, imp := range imports {
			ensureImport(f, imp)
		}
		sets = append(sets, &dst.ExprStmt{X: &dst.CallExpr{
			Fun:  &dst.SelectorExpr{X: dst.NewIdent("scene"), Sel: dst.NewIdent("Set")},
			Args: []dst.Expr{dst.NewIdent("c"), dst.NewIdent("e"), lit},
		}})
	}
	if needScene {
		ensureImport(f, sceneImportPath)
	}
	// Insert the scene.Set statements between `e := ...` and `return e`.
	if n := len(fn.Body.List); n > 0 && len(sets) > 0 {
		body := make([]dst.Stmt, 0, n+len(sets))
		body = append(body, fn.Body.List[:n-1]...)
		body = append(body, sets...)
		body = append(body, fn.Body.List[n-1])
		fn.Body.List = body
	}

	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, f); err != nil {
		return nil, nil, fmt.Errorf("srcmodel: print prefab: %w", err)
	}
	return buf.Bytes(), skipped, nil
}

// prefabFuncDecl returns the file's single top-level function declaration.
func prefabFuncDecl(f *dst.File) *dst.FuncDecl {
	for _, d := range f.Decls {
		if fn, ok := d.(*dst.FuncDecl); ok {
			return fn
		}
	}
	return nil
}
