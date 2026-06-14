package registry

import (
	"reflect"
	"strings"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/mlange-42/arche/ecs"
)

// FieldKind classifies a component field for the inspector: which widget edits
// it and how it is written back to source.
type FieldKind int

const (
	KindOpaque FieldKind = iota // recognized type but not editable
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
	KindSlice // slice of leaf-kind or struct elements
	KindColor // Vec3/Vec4 field that names a color: edited with a color picker
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

// isColorName reports whether a field named like this should be edited as a
// color rather than a raw vector. Position/direction/scale vectors keep their
// numeric drag widgets; only fields that read as a color get the picker.
func isColorName(name string) bool {
	switch strings.ToLower(name) {
	case "color", "tint", "albedo", "basecolor", "emissive", "emission",
		"ambient", "diffuse", "specular":
		return true
	}
	return strings.HasSuffix(strings.ToLower(name), "color")
}

// KindOf classifies t as a leaf editor kind (KindOpaque when t needs structural
// treatment — struct recursion or a slice widget — or is not editable at all).
func KindOf(t reflect.Type) FieldKind { return leafKind(t) }

// FieldsOf returns the inspector fields of an arbitrary struct type. The
// inspector uses it to render slice elements, which are not part of any
// registered component's precomputed field list.
func FieldsOf(t reflect.Type) []Field { return walkFields(t) }

// SliceEditable reports whether a slice of elem can be edited element-wise:
// leaf kinds get their leaf widget, structs get a nested field group.
func SliceEditable(elem reflect.Type) bool {
	return leafKind(elem) != KindOpaque || elem.Kind() == reflect.Struct
}

// maxFieldDepth bounds struct recursion in walkFields; anything nested deeper
// shows up as a single opaque entry.
const maxFieldDepth = 3

// walkFields flattens t's exported fields into inspector entries. Known math
// types are leaves; other structs are flattened recursively with dotted names
// (up to maxFieldDepth); slices of expressible elements become KindSlice;
// anything unrecognized becomes an opaque entry so the inspector can at least
// show that it exists.
func walkFields(t reflect.Type) []Field {
	if t.Kind() != reflect.Struct {
		return nil
	}
	return appendFields(nil, t, "", nil, 0)
}

func appendFields(out []Field, t reflect.Type, prefix string, index []int, depth int) []Field {
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		name := prefix + sf.Name
		idx := append(append([]int{}, index...), i)
		if k := leafKind(sf.Type); k != KindOpaque {
			if (k == KindVec3 || k == KindVec4) && isColorName(sf.Name) {
				k = KindColor
			}
			out = append(out, Field{Name: name, Kind: k, Index: idx})
			continue
		}
		switch {
		case sf.Type.Kind() == reflect.Slice && SliceEditable(sf.Type.Elem()):
			out = append(out, Field{Name: name, Kind: KindSlice, Index: idx})
		case sf.Type.Kind() == reflect.Struct && depth < maxFieldDepth:
			n := len(out)
			out = appendFields(out, sf.Type, name+".", idx, depth+1)
			if len(out) == n { // nothing inspectable inside: keep it visible
				out = append(out, Field{Name: name, Kind: KindOpaque, Index: idx})
			}
		default:
			out = append(out, Field{Name: name, Kind: KindOpaque, Index: idx})
		}
	}
	return out
}
