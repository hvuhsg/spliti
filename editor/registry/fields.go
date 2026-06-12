package registry

import (
	"reflect"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
)

// FieldKind classifies a component field for the inspector: which widget edits
// it and how it is written back to source.
type FieldKind int

const (
	KindOpaque FieldKind = iota // recognized type but not editable in M1
	KindFloat32
	KindFloat64
	KindInt // any signed/unsigned integer kind
	KindBool
	KindString
	KindVec2
	KindVec3
	KindVec4
	KindQuat
	KindEntity
)

// Field is one inspectable field of a component, possibly nested one struct
// level deep (Name then reads "Outer.Inner").
type Field struct {
	Name  string
	Kind  FieldKind
	Index []int // reflect FieldByIndex path from the component root
}

// Value resolves the field inside an addressable component value.
func (f Field) Value(comp reflect.Value) reflect.Value {
	return comp.FieldByIndex(f.Index)
}

var (
	vec2Type   = reflect.TypeFor[m.Vec2]()
	vec3Type   = reflect.TypeFor[m.Vec3]()
	vec4Type   = reflect.TypeFor[m.Vec4]()
	quatType   = reflect.TypeFor[m.Quat]()
	entityType = reflect.TypeFor[ecs.Entity]()
)

// leafKind classifies t as a leaf editor kind, or KindOpaque if it isn't one.
func leafKind(t reflect.Type) FieldKind {
	switch t {
	case vec2Type:
		return KindVec2
	case vec3Type:
		return KindVec3
	case vec4Type:
		return KindVec4
	case quatType:
		return KindQuat
	case entityType:
		return KindEntity
	}
	switch t.Kind() {
	case reflect.Float32:
		return KindFloat32
	case reflect.Float64:
		return KindFloat64
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return KindInt
	case reflect.Bool:
		return KindBool
	case reflect.String:
		return KindString
	}
	return KindOpaque
}

// walkFields flattens t's exported fields into inspector entries. Known math
// types are leaves; other structs are flattened one level with dotted names;
// anything deeper or unrecognized becomes a single opaque entry so the
// inspector can at least show that it exists.
func walkFields(t reflect.Type) []Field {
	if t.Kind() != reflect.Struct {
		return nil
	}
	var out []Field
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		if k := leafKind(sf.Type); k != KindOpaque {
			out = append(out, Field{Name: sf.Name, Kind: k, Index: []int{i}})
			continue
		}
		if sf.Type.Kind() == reflect.Struct {
			flat := false
			for j := 0; j < sf.Type.NumField(); j++ {
				nf := sf.Type.Field(j)
				if !nf.IsExported() {
					continue
				}
				if k := leafKind(nf.Type); k != KindOpaque {
					out = append(out, Field{
						Name:  sf.Name + "." + nf.Name,
						Kind:  k,
						Index: []int{i, j},
					})
					flat = true
				}
			}
			if flat {
				continue
			}
		}
		out = append(out, Field{Name: sf.Name, Kind: KindOpaque, Index: []int{i}})
	}
	return out
}
