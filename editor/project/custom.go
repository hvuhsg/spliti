package project

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/hvuhsg/spliti/editor/schema"
)

// LoadCustomComponents reads <projectDir>/components.toml. A missing
// file is treated as "no custom components" and returns an empty
// CustomComponentsFile, NOT an error — projects don't have to define
// any custom types.
func LoadCustomComponents(projectDir string) (*schema.CustomComponentsFile, error) {
	path := filepath.Join(projectDir, "components.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &schema.CustomComponentsFile{}, nil
		}
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	var f schema.CustomComponentsFile
	if _, err := toml.Decode(string(data), &f); err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return &f, nil
}

// SaveCustomComponents writes f to <projectDir>/components.toml
// atomically. Pass nil/empty f to leave the file empty (still writes
// it so a future LoadCustomComponents finds zero schemas — same
// effect as a missing file, but explicit).
func SaveCustomComponents(projectDir string, f *schema.CustomComponentsFile) error {
	path := filepath.Join(projectDir, "components.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if f == nil {
		f = &schema.CustomComponentsFile{}
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = "  "
	if err := enc.Encode(f); err != nil {
		return fmt.Errorf("encode custom components: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
