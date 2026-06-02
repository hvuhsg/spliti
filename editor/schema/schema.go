// Package schema holds the on-disk shape of user-defined component
// types. It's a leaf package so both editor/components (which builds
// runtime ComponentDescs from schemas) and editor/project (which
// reads/writes components.toml) can import it without a cycle.
package schema

// CustomFieldSchema is one field on a user-defined component type.
// Kind is one of "int", "string", "bool", "rune" — the same axes the
// inspector knows how to edit.
type CustomFieldSchema struct {
	Name string `toml:"name"`
	Kind string `toml:"kind"`
}

// CustomComponentSchema is one user-defined component type. Name is
// also the on-disk component name in scene files.
type CustomComponentSchema struct {
	Name   string              `toml:"name"`
	Fields []CustomFieldSchema `toml:"field"`
}

// CustomComponentsFile is the project-level <projectDir>/components.toml
// document.
type CustomComponentsFile struct {
	Components []CustomComponentSchema `toml:"component"`
}
