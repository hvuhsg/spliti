package project

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

// SceneFile is the on-disk format for one scene. It is intentionally
// component-agnostic: each entity carries a map keyed by component name,
// and the value is whatever TOML sub-table the component's encoder produced.
//
// At Phase F the editor's component registry will own the encoders/decoders;
// scene.go itself never names a specific component type. That separation
// keeps the file loader stable while the editor's component vocabulary
// evolves.
//
// On-disk shape:
//
//	schema = 1
//	name   = "main"
//
//	[[entity]]
//	name = "left_paddle"
//	[entity.position]
//	x = 2
//	y = 5
//	[entity.glyph]
//	char = "█"
//	[entity.glyph.style]
//	fg = "yellow"
//	bold = true
//
// The map used internally is map[string]any. BurntSushi/toml emits nested
// tables and arrays from interface values transparently; primitives
// (int, string, bool, float, []any, map[string]any) are all supported.
type SceneFile struct {
	Schema   int            `toml:"schema"`
	Name     string         `toml:"name"`
	Entities []SceneEntity  `toml:"entity"`
}

// SceneEntity is one entity in a scene.
type SceneEntity struct {
	Name       string         `toml:"name"`
	Components map[string]any `toml:",inline"`
}

// LoadSceneFile reads and parses a .scene file. Component sub-tables are
// returned as map[string]any so callers (the registry) can decode each one
// into its concrete Go type.
func LoadSceneFile(path string) (*SceneFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scene %q: %w", path, err)
	}
	return ParseScene(data)
}

// ParseScene parses scene-file bytes. Exposed for tests + in-memory loaders
// that don't have a path (the editor's "Reload" button could pull from a
// buffer in the future).
func ParseScene(data []byte) (*SceneFile, error) {
	// We can't use BurntSushi/toml's struct decoder directly with the
	// inline ",inline" map field — TOML's data model says the entity's
	// component sub-tables become nested maps in a map[string]any value
	// for the entity, but we want them flattened on the SceneEntity.
	//
	// Strategy: decode into a generic map[string]any first, then walk it
	// to build SceneEntity values. This is the same pattern Go's
	// encoding/json uses for "lift sub-tables to top-level fields".
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("parse scene: %w", err)
	}
	sf := &SceneFile{}
	if v, ok := raw["schema"]; ok {
		if n, ok := v.(int64); ok {
			sf.Schema = int(n)
		}
	}
	if v, ok := raw["name"]; ok {
		if s, ok := v.(string); ok {
			sf.Name = s
		}
	}
	if v, ok := raw["entity"]; ok {
		entries, ok := v.([]map[string]any)
		if !ok {
			return nil, fmt.Errorf("scene: 'entity' must be an array of tables, got %T", v)
		}
		for _, ent := range entries {
			se := SceneEntity{Components: map[string]any{}}
			for k, val := range ent {
				if k == "name" {
					if s, ok := val.(string); ok {
						se.Name = s
					}
					continue
				}
				se.Components[k] = val
			}
			sf.Entities = append(sf.Entities, se)
		}
	}
	if sf.Schema == 0 {
		sf.Schema = 1
	}
	return sf, nil
}

// SaveSceneFile writes a SceneFile to disk atomically. Map iteration order
// is unstable in Go, so we serialize component keys in a deterministic
// (alphabetical) order — golden-file diffs would otherwise flap.
func SaveSceneFile(path string, sf *SceneFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := EncodeScene(sf)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EncodeScene serializes a SceneFile to TOML bytes deterministically.
func EncodeScene(sf *SceneFile) ([]byte, error) {
	// We assemble a top-level map and let BurntSushi/toml encode it.
	// Component keys are extracted from each entity and re-attached in
	// alphabetical order to keep diffs stable.
	top := map[string]any{
		"schema": int64(orDefault(sf.Schema, 1)),
		"name":   sf.Name,
	}
	entities := make([]map[string]any, 0, len(sf.Entities))
	for _, e := range sf.Entities {
		ent := map[string]any{}
		if e.Name != "" {
			ent["name"] = e.Name
		}
		// Sort component keys for deterministic output.
		keys := make([]string, 0, len(e.Components))
		for k := range e.Components {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ent[k] = e.Components[k]
		}
		entities = append(entities, ent)
	}
	if len(entities) > 0 {
		top["entity"] = entities
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = "  "
	if err := enc.Encode(top); err != nil {
		return nil, fmt.Errorf("encode scene: %w", err)
	}
	return buf.Bytes(), nil
}

func orDefault(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}
