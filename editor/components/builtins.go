package components

import (
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/project"
	"github.com/hvuhsg/spliti/plugin/runtime"
	"github.com/hvuhsg/spliti/plugin/sprite"
	"github.com/hvuhsg/spliti/plugin/tui"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/generic"
)

// RegisterBuiltins installs descriptors for every built-in component.
// Call once during editor setup. Subsequent calls panic via Register's
// duplicate-name guard.
//
// The on-disk component name (used in scene files and inspector titles)
// is camelCase: "position", "sprite", "keyboardControl". Type names in
// Go are PascalCase but those are private to the editor.
func RegisterBuiltins() {
	registerPosition()
	registerGlyph()
	registerLayer()
	registerSprite()
	registerVelocity()
	registerBounds()
	registerTag()
	registerKeyboardControl()
}

// --- helpers ------------------------------------------------------------

// addOne / removeOne / getOne wrap arche's generic Map1 methods. We need
// these because Map1 methods are pointer-receiver and the value returned
// by NewMap1 isn't addressable when used inline (closures everywhere).
// Cheap helpers; arche caches the underlying component IDs.
func addOne[T any](w *ecs.World, e ecs.Entity) {
	m := generic.NewMap1[T](w)
	m.Add(e)
}

func removeOne[T any](w *ecs.World, e ecs.Entity) {
	m := generic.NewMap1[T](w)
	m.Remove(e)
}

func getOne[T any](w *ecs.World, e ecs.Entity) *T {
	m := generic.NewMap1[T](w)
	return m.Get(e)
}

// hasOne reports whether the entity has component T.
func hasOne[T any](w *ecs.World, e ecs.Entity) bool {
	return w.Has(e, ecs.ComponentID[T](w))
}

// --- Position -----------------------------------------------------------

func registerPosition() {
	Register(&ComponentDesc{
		Name: "position",
		Has:  func(w *ecs.World, e ecs.Entity) bool { return hasOne[tui.Position](w, e) },
		Add: func(c *app.Ctx, e ecs.Entity) {
			addOne[tui.Position](c.World(), e)
		},
		Remove: func(c *app.Ctx, e ecs.Entity) {
			removeOne[tui.Position](c.World(), e)
		},
		Fields: []Field{
			{
				Name: "X", Kind: FieldInt,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[tui.Position](c.World(), e).X },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					getOne[tui.Position](c.World(), e).X = asInt(v)
				},
			},
			{
				Name: "Y", Kind: FieldInt,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[tui.Position](c.World(), e).Y },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					getOne[tui.Position](c.World(), e).Y = asInt(v)
				},
			},
		},
		Encode: func(w *ecs.World, e ecs.Entity) (map[string]any, bool) {
			if !hasOne[tui.Position](w, e) {
				return nil, false
			}
			p := getOne[tui.Position](w, e)
			return map[string]any{"x": int64(p.X), "y": int64(p.Y)}, true
		},
		Decode: func(c *app.Ctx, e ecs.Entity, val map[string]any) error {
			if !hasOne[tui.Position](c.World(), e) {
				addOne[tui.Position](c.World(), e)
			}
			p := getOne[tui.Position](c.World(), e)
			p.X, p.Y = asInt(val["x"]), asInt(val["y"])
			return nil
		},
	})
}

// --- Glyph --------------------------------------------------------------

func registerGlyph() {
	Register(&ComponentDesc{
		Name: "glyph",
		Has:  func(w *ecs.World, e ecs.Entity) bool { return hasOne[tui.Glyph](w, e) },
		Add: func(c *app.Ctx, e ecs.Entity) {
			addOne[tui.Glyph](c.World(), e)
			getOne[tui.Glyph](c.World(), e).Char = '?'
		},
		Remove: func(c *app.Ctx, e ecs.Entity) {
			removeOne[tui.Glyph](c.World(), e)
		},
		Fields: []Field{
			{
				Name: "Char", Kind: FieldRune,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[tui.Glyph](c.World(), e).Char },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					if r, ok := v.(rune); ok {
						getOne[tui.Glyph](c.World(), e).Char = r
					}
				},
			},
			{
				Name: "Style", Kind: FieldStyle,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[tui.Glyph](c.World(), e).Style },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					if st, ok := v.(tcell.Style); ok {
						getOne[tui.Glyph](c.World(), e).Style = st
					}
				},
			},
		},
		Encode: func(w *ecs.World, e ecs.Entity) (map[string]any, bool) {
			if !hasOne[tui.Glyph](w, e) {
				return nil, false
			}
			g := getOne[tui.Glyph](w, e)
			out := map[string]any{"char": string(g.Char)}
			st := project.EncodeStyle(g.Style)
			if !st.IsZero() {
				out["style"] = styleTOMLToMap(st)
			}
			return out, true
		},
		Decode: func(c *app.Ctx, e ecs.Entity, val map[string]any) error {
			if !hasOne[tui.Glyph](c.World(), e) {
				addOne[tui.Glyph](c.World(), e)
			}
			g := getOne[tui.Glyph](c.World(), e)
			g.Char = asRune(val["char"])
			if styleVal, ok := val["style"].(map[string]any); ok {
				st := mapToStyleTOML(styleVal)
				if out, err := project.DecodeStyle(st); err == nil {
					g.Style = out
				}
			} else {
				g.Style = tcell.StyleDefault
			}
			return nil
		},
	})
}

// --- Layer --------------------------------------------------------------

func registerLayer() {
	Register(&ComponentDesc{
		Name: "layer",
		Has:  func(w *ecs.World, e ecs.Entity) bool { return hasOne[tui.Layer](w, e) },
		Add: func(c *app.Ctx, e ecs.Entity) {
			addOne[tui.Layer](c.World(), e)
		},
		Remove: func(c *app.Ctx, e ecs.Entity) {
			removeOne[tui.Layer](c.World(), e)
		},
		Fields: []Field{
			{
				Name: "Z", Kind: FieldInt,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[tui.Layer](c.World(), e).Z },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					getOne[tui.Layer](c.World(), e).Z = asInt(v)
				},
			},
		},
		Encode: func(w *ecs.World, e ecs.Entity) (map[string]any, bool) {
			if !hasOne[tui.Layer](w, e) {
				return nil, false
			}
			return map[string]any{"z": int64(getOne[tui.Layer](w, e).Z)}, true
		},
		Decode: func(c *app.Ctx, e ecs.Entity, val map[string]any) error {
			if !hasOne[tui.Layer](c.World(), e) {
				addOne[tui.Layer](c.World(), e)
			}
			getOne[tui.Layer](c.World(), e).Z = asInt(val["z"])
			return nil
		},
	})
}

// --- Sprite -------------------------------------------------------------

func registerSprite() {
	Register(&ComponentDesc{
		Name: "sprite",
		Has:  func(w *ecs.World, e ecs.Entity) bool { return hasOne[sprite.Sprite](w, e) },
		Add: func(c *app.Ctx, e ecs.Entity) {
			addOne[sprite.Sprite](c.World(), e)
		},
		Remove: func(c *app.Ctx, e ecs.Entity) {
			removeOne[sprite.Sprite](c.World(), e)
		},
		Fields: []Field{
			{
				Name: "Ref", Kind: FieldString,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[sprite.Sprite](c.World(), e).Ref },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					if s, ok := v.(string); ok {
						getOne[sprite.Sprite](c.World(), e).Ref = s
					}
				},
			},
			{
				Name: "AnchorX", Kind: FieldInt,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[sprite.Sprite](c.World(), e).AnchorX },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					getOne[sprite.Sprite](c.World(), e).AnchorX = asInt(v)
				},
			},
			{
				Name: "AnchorY", Kind: FieldInt,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[sprite.Sprite](c.World(), e).AnchorY },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					getOne[sprite.Sprite](c.World(), e).AnchorY = asInt(v)
				},
			},
		},
		Encode: func(w *ecs.World, e ecs.Entity) (map[string]any, bool) {
			if !hasOne[sprite.Sprite](w, e) {
				return nil, false
			}
			s := getOne[sprite.Sprite](w, e)
			out := map[string]any{"ref": s.Ref}
			if s.AnchorX != 0 {
				out["anchorX"] = int64(s.AnchorX)
			}
			if s.AnchorY != 0 {
				out["anchorY"] = int64(s.AnchorY)
			}
			return out, true
		},
		Decode: func(c *app.Ctx, e ecs.Entity, val map[string]any) error {
			if !hasOne[sprite.Sprite](c.World(), e) {
				addOne[sprite.Sprite](c.World(), e)
			}
			s := getOne[sprite.Sprite](c.World(), e)
			s.Ref, _ = val["ref"].(string)
			s.AnchorX = asInt(val["anchorX"])
			s.AnchorY = asInt(val["anchorY"])
			return nil
		},
	})
}

// --- Velocity -----------------------------------------------------------

func registerVelocity() {
	Register(&ComponentDesc{
		Name: "velocity",
		Has:  func(w *ecs.World, e ecs.Entity) bool { return hasOne[runtime.Velocity](w, e) },
		Add: func(c *app.Ctx, e ecs.Entity) {
			addOne[runtime.Velocity](c.World(), e)
		},
		Remove: func(c *app.Ctx, e ecs.Entity) {
			removeOne[runtime.Velocity](c.World(), e)
		},
		Fields: []Field{
			{
				Name: "DX", Kind: FieldInt,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[runtime.Velocity](c.World(), e).DX },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					getOne[runtime.Velocity](c.World(), e).DX = asInt(v)
				},
			},
			{
				Name: "DY", Kind: FieldInt,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[runtime.Velocity](c.World(), e).DY },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					getOne[runtime.Velocity](c.World(), e).DY = asInt(v)
				},
			},
		},
		Encode: func(w *ecs.World, e ecs.Entity) (map[string]any, bool) {
			if !hasOne[runtime.Velocity](w, e) {
				return nil, false
			}
			v := getOne[runtime.Velocity](w, e)
			return map[string]any{"dx": int64(v.DX), "dy": int64(v.DY)}, true
		},
		Decode: func(c *app.Ctx, e ecs.Entity, val map[string]any) error {
			if !hasOne[runtime.Velocity](c.World(), e) {
				addOne[runtime.Velocity](c.World(), e)
			}
			v := getOne[runtime.Velocity](c.World(), e)
			v.DX, v.DY = asInt(val["dx"]), asInt(val["dy"])
			return nil
		},
	})
}

// --- Bounds -------------------------------------------------------------

func registerBounds() {
	Register(&ComponentDesc{
		Name: "bounds",
		Has:  func(w *ecs.World, e ecs.Entity) bool { return hasOne[runtime.Bounds](w, e) },
		Add: func(c *app.Ctx, e ecs.Entity) {
			addOne[runtime.Bounds](c.World(), e)
			b := getOne[runtime.Bounds](c.World(), e)
			b.W, b.H = 1, 1
		},
		Remove: func(c *app.Ctx, e ecs.Entity) {
			removeOne[runtime.Bounds](c.World(), e)
		},
		Fields: []Field{
			{
				Name: "W", Kind: FieldInt,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[runtime.Bounds](c.World(), e).W },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					getOne[runtime.Bounds](c.World(), e).W = asInt(v)
				},
			},
			{
				Name: "H", Kind: FieldInt,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[runtime.Bounds](c.World(), e).H },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					getOne[runtime.Bounds](c.World(), e).H = asInt(v)
				},
			},
		},
		Encode: func(w *ecs.World, e ecs.Entity) (map[string]any, bool) {
			if !hasOne[runtime.Bounds](w, e) {
				return nil, false
			}
			b := getOne[runtime.Bounds](w, e)
			return map[string]any{"w": int64(b.W), "h": int64(b.H)}, true
		},
		Decode: func(c *app.Ctx, e ecs.Entity, val map[string]any) error {
			if !hasOne[runtime.Bounds](c.World(), e) {
				addOne[runtime.Bounds](c.World(), e)
			}
			b := getOne[runtime.Bounds](c.World(), e)
			b.W, b.H = asInt(val["w"]), asInt(val["h"])
			return nil
		},
	})
}

// --- Tag ----------------------------------------------------------------

func registerTag() {
	Register(&ComponentDesc{
		Name: "tag",
		Has:  func(w *ecs.World, e ecs.Entity) bool { return hasOne[runtime.Tag](w, e) },
		Add: func(c *app.Ctx, e ecs.Entity) {
			addOne[runtime.Tag](c.World(), e)
		},
		Remove: func(c *app.Ctx, e ecs.Entity) {
			removeOne[runtime.Tag](c.World(), e)
		},
		Fields: []Field{
			{
				Name: "Name", Kind: FieldString,
				Get: func(c *app.Ctx, e ecs.Entity) any { return getOne[runtime.Tag](c.World(), e).Name },
				Set: func(c *app.Ctx, e ecs.Entity, v any) {
					if s, ok := v.(string); ok {
						getOne[runtime.Tag](c.World(), e).Name = s
					}
				},
			},
		},
		Encode: func(w *ecs.World, e ecs.Entity) (map[string]any, bool) {
			if !hasOne[runtime.Tag](w, e) {
				return nil, false
			}
			return map[string]any{"name": getOne[runtime.Tag](w, e).Name}, true
		},
		Decode: func(c *app.Ctx, e ecs.Entity, val map[string]any) error {
			if !hasOne[runtime.Tag](c.World(), e) {
				addOne[runtime.Tag](c.World(), e)
			}
			getOne[runtime.Tag](c.World(), e).Name, _ = val["name"].(string)
			return nil
		},
	})
}

// --- KeyboardControl ----------------------------------------------------

func registerKeyboardControl() {
	Register(&ComponentDesc{
		Name: "keyboardControl",
		Has:  func(w *ecs.World, e ecs.Entity) bool { return hasOne[runtime.KeyboardControl](w, e) },
		Add: func(c *app.Ctx, e ecs.Entity) {
			addOne[runtime.KeyboardControl](c.World(), e)
		},
		Remove: func(c *app.Ctx, e ecs.Entity) {
			removeOne[runtime.KeyboardControl](c.World(), e)
		},
		Fields: []Field{
			{
				Name: "Bindings", Kind: FieldKeyBindings,
				Get: func(c *app.Ctx, e ecs.Entity) any {
					return getOne[runtime.KeyboardControl](c.World(), e).Bindings
				},
				Set: func(_ *app.Ctx, _ ecs.Entity, _ any) {}, // edited via specialized UI in v2
			},
		},
		Encode: func(w *ecs.World, e ecs.Entity) (map[string]any, bool) {
			if !hasOne[runtime.KeyboardControl](w, e) {
				return nil, false
			}
			kc := getOne[runtime.KeyboardControl](w, e)
			bindings := make([]map[string]any, 0, len(kc.Bindings))
			for _, b := range kc.Bindings {
				m := map[string]any{
					"dx":        int64(b.DX),
					"dy":        int64(b.DY),
					"holdTicks": int64(b.HoldTicks),
				}
				if b.Key == tcell.KeyRune {
					m["key"] = "rune"
					m["rune"] = string(b.Rune)
				} else {
					m["key"] = keyName(b.Key)
				}
				bindings = append(bindings, m)
			}
			return map[string]any{"bindings": bindings}, true
		},
		Decode: func(c *app.Ctx, e ecs.Entity, val map[string]any) error {
			if !hasOne[runtime.KeyboardControl](c.World(), e) {
				addOne[runtime.KeyboardControl](c.World(), e)
			}
			kc := getOne[runtime.KeyboardControl](c.World(), e)
			kc.Bindings = nil
			raw, _ := val["bindings"].([]map[string]any)
			for _, b := range raw {
				kb := runtime.KeyBinding{
					DX:        asInt(b["dx"]),
					DY:        asInt(b["dy"]),
					HoldTicks: asInt(b["holdTicks"]),
					Rune:      asRune(b["rune"]),
				}
				key, _ := b["key"].(string)
				kb.Key = parseKeyName(key)
				kc.Bindings = append(kc.Bindings, kb)
			}
			return nil
		},
	})
}

// --- value coercion -----------------------------------------------------

func asInt(v any) int {
	switch x := v.(type) {
	case int64:
		return int(x)
	case int:
		return x
	case float64:
		return int(x)
	}
	return 0
}

func asRune(v any) rune {
	if s, ok := v.(string); ok {
		for _, r := range s {
			return r
		}
	}
	if r, ok := v.(rune); ok {
		return r
	}
	return ' '
}

// --- style codec wrappers -----------------------------------------------

// styleTOMLToMap converts the StyleTOML struct into the map[string]any
// shape we hand to BurntSushi/toml so it can serialize.
func styleTOMLToMap(s project.StyleTOML) map[string]any {
	m := map[string]any{}
	if s.Fg != "" {
		m["fg"] = s.Fg
	}
	if s.Bg != "" {
		m["bg"] = s.Bg
	}
	if s.Bold {
		m["bold"] = true
	}
	if s.Italic {
		m["italic"] = true
	}
	if s.Underline {
		m["underline"] = true
	}
	if s.Reverse {
		m["reverse"] = true
	}
	return m
}

// mapToStyleTOML inverts styleTOMLToMap for decoding.
func mapToStyleTOML(m map[string]any) project.StyleTOML {
	out := project.StyleTOML{}
	if v, ok := m["fg"].(string); ok {
		out.Fg = v
	}
	if v, ok := m["bg"].(string); ok {
		out.Bg = v
	}
	if v, ok := m["bold"].(bool); ok {
		out.Bold = v
	}
	if v, ok := m["italic"].(bool); ok {
		out.Italic = v
	}
	if v, ok := m["underline"].(bool); ok {
		out.Underline = v
	}
	if v, ok := m["reverse"].(bool); ok {
		out.Reverse = v
	}
	return out
}

// --- key-name codec -----------------------------------------------------

// keyName / parseKeyName convert tcell.Key ↔ short string for scene
// serialization. Only the keys an editor user is likely to bind are
// covered; unknown values fall back to KeyRune.
var keyNames = map[tcell.Key]string{
	tcell.KeyUp: "Up", tcell.KeyDown: "Down", tcell.KeyLeft: "Left", tcell.KeyRight: "Right",
	tcell.KeyEnter: "Enter", tcell.KeyTab: "Tab", tcell.KeyEscape: "Escape",
	tcell.KeyBackspace: "Backspace", tcell.KeyHome: "Home", tcell.KeyEnd: "End",
}

func keyName(k tcell.Key) string {
	if s, ok := keyNames[k]; ok {
		return s
	}
	return "rune"
}

func parseKeyName(s string) tcell.Key {
	for k, name := range keyNames {
		if name == s {
			return k
		}
	}
	return tcell.KeyRune
}

// SortedNames returns registry component names alphabetically. Used by
// "Add component" menu so the UI is stable across runs.
func SortedNames() []string {
	names := make([]string, 0, len(registry))
	for _, d := range registry {
		names = append(names, d.Name)
	}
	sort.Strings(names)
	return names
}
