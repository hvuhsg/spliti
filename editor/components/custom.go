package components

import (
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/schema"
	"github.com/mlange-42/arche/ecs"
)

// Re-exports from editor/schema for callers that already imported
// editor/components. Keeps the original API surface unchanged after
// moving the types to break the import cycle.
type (
	CustomFieldSchema     = schema.CustomFieldSchema
	CustomComponentSchema = schema.CustomComponentSchema
	CustomComponentsFile  = schema.CustomComponentsFile
)

// CustomData is the single arche component every entity carries when
// it has any user-defined component values. Values is keyed by schema
// name; the inner map is keyed by field name; field values are stored
// boxed in any with the kind matching the schema.
//
// Storing all custom values in one component (instead of one component
// per schema) means we don't dynamically register Go types with arche —
// arche only ever sees CustomData. The editor's registry synthesises
// per-schema ComponentDescs that read/write through CustomData.
type CustomData struct {
	Values map[string]map[string]any
}

// schemas is the active set of custom schemas. Kept in sync with the
// registry's custom ComponentDesc entries.
var schemas []CustomComponentSchema

// CustomSchemas returns the current schemas (read-only — callers must
// not mutate).
func CustomSchemas() []CustomComponentSchema { return schemas }

// RegisterCustom registers a synthetic ComponentDesc in the registry
// for each schema in f. Re-call to refresh after schemas change; old
// custom descs (those not in f.Components) are unregistered first.
func RegisterCustom(f *CustomComponentsFile) {
	// Unregister previous custom descs.
	for _, s := range schemas {
		Unregister(s.Name)
	}
	schemas = nil
	if f == nil {
		return
	}
	for _, s := range f.Components {
		Register(makeCustomDesc(s))
		schemas = append(schemas, s)
	}
}

// makeCustomDesc constructs the synthetic ComponentDesc for one
// schema. Value capture: we copy the schema into the closure so future
// schema-list mutations don't change this desc's behaviour — a desc
// always reflects the schema it was built from.
func makeCustomDesc(s CustomComponentSchema) *ComponentDesc {
	return &ComponentDesc{
		Name:   s.Name,
		Has:    func(w *ecs.World, e ecs.Entity) bool { return customHas(w, e, s.Name) },
		Add:    func(c *app.Ctx, e ecs.Entity) { customAdd(c, e, s) },
		Remove: func(c *app.Ctx, e ecs.Entity) { customRemove(c, e, s.Name) },
		Fields: makeCustomFields(s),
		Encode: func(w *ecs.World, e ecs.Entity) (map[string]any, bool) {
			return customEncode(w, e, s)
		},
		Decode: func(c *app.Ctx, e ecs.Entity, val map[string]any) error {
			return customDecode(c, e, s, val)
		},
	}
}

func customHas(w *ecs.World, e ecs.Entity, name string) bool {
	if !hasOne[CustomData](w, e) {
		return false
	}
	cd := getOne[CustomData](w, e)
	_, ok := cd.Values[name]
	return ok
}

func customAdd(c *app.Ctx, e ecs.Entity, s CustomComponentSchema) {
	w := c.World()
	if !hasOne[CustomData](w, e) {
		addOne[CustomData](w, e)
	}
	cd := getOne[CustomData](w, e)
	if cd.Values == nil {
		cd.Values = map[string]map[string]any{}
	}
	values := map[string]any{}
	for _, f := range s.Fields {
		values[f.Name] = defaultForKind(f.Kind)
	}
	cd.Values[s.Name] = values
}

func customRemove(c *app.Ctx, e ecs.Entity, name string) {
	w := c.World()
	if !hasOne[CustomData](w, e) {
		return
	}
	cd := getOne[CustomData](w, e)
	delete(cd.Values, name)
}

func customEncode(w *ecs.World, e ecs.Entity, s CustomComponentSchema) (map[string]any, bool) {
	if !hasOne[CustomData](w, e) {
		return nil, false
	}
	cd := getOne[CustomData](w, e)
	field, ok := cd.Values[s.Name]
	if !ok {
		return nil, false
	}
	out := make(map[string]any, len(field))
	for _, f := range s.Fields {
		v := field[f.Name]
		if v == nil {
			v = defaultForKind(f.Kind)
		}
		out[f.Name] = encodeValue(f.Kind, v)
	}
	return out, true
}

func customDecode(c *app.Ctx, e ecs.Entity, s CustomComponentSchema, val map[string]any) error {
	w := c.World()
	if !hasOne[CustomData](w, e) {
		addOne[CustomData](w, e)
	}
	cd := getOne[CustomData](w, e)
	if cd.Values == nil {
		cd.Values = map[string]map[string]any{}
	}
	values := map[string]any{}
	for _, f := range s.Fields {
		values[f.Name] = coerceForKind(f.Kind, val[f.Name])
	}
	cd.Values[s.Name] = values
	return nil
}

func makeCustomFields(s CustomComponentSchema) []Field {
	out := make([]Field, 0, len(s.Fields))
	for _, f := range s.Fields {
		f := f // capture
		out = append(out, Field{
			Name: f.Name,
			Kind: parseFieldKind(f.Kind),
			Get: func(c *app.Ctx, e ecs.Entity) any {
				if !hasOne[CustomData](c.World(), e) {
					return defaultForKind(f.Kind)
				}
				cd := getOne[CustomData](c.World(), e)
				vals, ok := cd.Values[s.Name]
				if !ok {
					return defaultForKind(f.Kind)
				}
				v, ok := vals[f.Name]
				if !ok {
					return defaultForKind(f.Kind)
				}
				return v
			},
			Set: func(c *app.Ctx, e ecs.Entity, v any) {
				if !hasOne[CustomData](c.World(), e) {
					return
				}
				cd := getOne[CustomData](c.World(), e)
				vals, ok := cd.Values[s.Name]
				if !ok {
					return
				}
				vals[f.Name] = coerceForKind(f.Kind, v)
			},
		})
	}
	return out
}

// --- helpers: kind → default / coerce / encode --------------------------

func defaultForKind(kind string) any {
	switch kind {
	case "int":
		return 0
	case "string":
		return ""
	case "bool":
		return false
	case "rune":
		return ' '
	}
	return ""
}

// coerceForKind takes a value of unknown shape (came from TOML decode
// or from a Field.Set call) and returns a concrete typed value.
// Tolerates the common widenings: TOML decodes to int64; ints from the
// inspector come in as int.
func coerceForKind(kind string, v any) any {
	switch kind {
	case "int":
		switch x := v.(type) {
		case int64:
			return int(x)
		case int:
			return x
		case float64:
			return int(x)
		}
		return 0
	case "string":
		if s, ok := v.(string); ok {
			return s
		}
		return ""
	case "bool":
		if b, ok := v.(bool); ok {
			return b
		}
		return false
	case "rune":
		switch x := v.(type) {
		case rune:
			return x
		case string:
			for _, r := range x {
				return r
			}
		}
		return ' '
	}
	return v
}

// encodeValue produces the serializable form for TOML output. ints
// become int64 to match BurntSushi/toml's emitter; runes become
// single-character strings.
func encodeValue(kind string, v any) any {
	switch kind {
	case "int":
		if n, ok := v.(int); ok {
			return int64(n)
		}
		if n, ok := v.(int64); ok {
			return n
		}
		return int64(0)
	case "rune":
		if r, ok := v.(rune); ok {
			return string(r)
		}
		return ""
	}
	return v
}

func parseFieldKind(kind string) FieldKind {
	switch kind {
	case "int":
		return FieldInt
	case "string":
		return FieldString
	case "bool":
		return FieldBool
	case "rune":
		return FieldRune
	}
	return FieldString
}
