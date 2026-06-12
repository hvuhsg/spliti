package registry

import "reflect"

// Components used to be plain value structs, where a value copy IS a deep
// copy. With slices (and in principle maps/pointers) in components, Value and
// SetValue must deep-copy: a captured undo "before" or play-mode snapshot
// sharing a backing array with live storage would be silently corrupted by
// in-place element edits.

// typeNeedsDeepCopy reports whether values of t can share memory after a
// shallow copy.
func typeNeedsDeepCopy(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Slice, reflect.Map, reflect.Pointer:
		return true
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if typeNeedsDeepCopy(t.Field(i).Type) {
				return true
			}
		}
	case reflect.Array:
		return typeNeedsDeepCopy(t.Elem())
	}
	return false
}

// deepCopyAny returns a copy of val sharing no mutable memory with it.
// Unexported struct fields are copied shallowly (reflect cannot write them);
// component types are expected to keep reference-typed state exported.
func deepCopyAny(val any) any {
	src := reflect.ValueOf(val)
	dst := reflect.New(src.Type()).Elem()
	deepCopyInto(dst, src)
	return dst.Interface()
}

func deepCopyInto(dst, src reflect.Value) {
	switch src.Kind() {
	case reflect.Slice:
		if src.IsNil() {
			return
		}
		dst.Set(reflect.MakeSlice(src.Type(), src.Len(), src.Len()))
		for i := 0; i < src.Len(); i++ {
			deepCopyInto(dst.Index(i), src.Index(i))
		}
	case reflect.Map:
		if src.IsNil() {
			return
		}
		dst.Set(reflect.MakeMap(src.Type()))
		iter := src.MapRange()
		for iter.Next() {
			ev := reflect.New(src.Type().Elem()).Elem()
			deepCopyInto(ev, iter.Value())
			dst.SetMapIndex(iter.Key(), ev)
		}
	case reflect.Pointer:
		if src.IsNil() {
			return
		}
		dst.Set(reflect.New(src.Type().Elem()))
		deepCopyInto(dst.Elem(), src.Elem())
	case reflect.Struct:
		dst.Set(src)
		t := src.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.IsExported() && typeNeedsDeepCopy(f.Type) {
				deepCopyInto(dst.Field(i), src.Field(i))
			}
		}
	case reflect.Array:
		dst.Set(src)
		if typeNeedsDeepCopy(src.Type().Elem()) {
			for i := 0; i < src.Len(); i++ {
				deepCopyInto(dst.Index(i), src.Index(i))
			}
		}
	default:
		dst.Set(src)
	}
}
