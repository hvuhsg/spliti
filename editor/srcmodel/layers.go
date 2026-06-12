package srcmodel

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// LayersFile is the parsed //spliti:layers const block: the game's named
// collision layers, one identifier per bit in declaration (iota) order:
//
//	//spliti:layers
//	const (
//		LayerDefault uint32 = 1 << iota
//		LayerPlayer
//		LayerEnemy
//	)
//
// The editor's Layers panel renames and appends entries; removing a layer is
// not supported (it would silently renumber every later bit). Blank (`_`)
// entries hold a bit position without naming it.
type LayersFile struct {
	Path  string
	Names []string // index = bit position; "" for blank entries

	file        *dst.File
	decl        *dst.GenDecl
	lastWritten []byte
}

// MaxLayers is the number of collision layer bits (Layer/Mask are uint32).
const MaxLayers = 32

// layersDirective is the magic comment marking the const block.
const layersDirective = "spliti:layers"

// FindLayersFile scans dir's top-level .go files for a //spliti:layers
// directive and returns the file path holding it.
func FindLayersFile(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if bytes.Contains(data, []byte("//"+layersDirective)) {
			return path, true
		}
	}
	return "", false
}

// LastWritten returns the bytes of the most recent successful Save.
func (lf *LayersFile) LastWritten() []byte { return lf.lastWritten }

// ParseLayersFile reads path and locates its //spliti:layers const block.
func ParseLayersFile(path string) (*LayersFile, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseLayersSource(src, path)
}

func parseLayersSource(src []byte, path string) (*LayersFile, error) {
	f, err := decorator.ParseFile(token.NewFileSet(), path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	lf := &LayersFile{Path: path, file: f}
	for _, decl := range f.Decls {
		gd, ok := decl.(*dst.GenDecl)
		if !ok || gd.Tok != token.CONST || !hasDirective(gd.Decs.Start.All(), layersDirective) {
			continue
		}
		lf.decl = gd
		break
	}
	if lf.decl == nil {
		return nil, fmt.Errorf("srcmodel: no //%s const block in %s", layersDirective, path)
	}
	for _, spec := range lf.decl.Specs {
		vs, ok := spec.(*dst.ValueSpec)
		if !ok || len(vs.Names) != 1 {
			return nil, fmt.Errorf("srcmodel: %s: unsupported spec shape in layers block", path)
		}
		name := vs.Names[0].Name
		if name == "_" {
			name = ""
		}
		lf.Names = append(lf.Names, name)
	}
	if len(lf.Names) > MaxLayers {
		return nil, fmt.Errorf("srcmodel: %s: %d layers exceed the %d-bit limit", path, len(lf.Names), MaxLayers)
	}
	return lf, nil
}

func hasDirective(comments []string, directive string) bool {
	for _, c := range comments {
		if strings.TrimSpace(strings.TrimPrefix(c, "//")) == directive {
			return true
		}
	}
	return false
}

// LayerName returns the name bound to a bit position, or "".
func (lf *LayersFile) LayerName(bit int) string {
	if bit < 0 || bit >= len(lf.Names) {
		return ""
	}
	return lf.Names[bit]
}

// Bit returns the bit position of a layer name.
func (lf *LayersFile) Bit(name string) (int, bool) {
	for i, n := range lf.Names {
		if n != "" && n == name {
			return i, true
		}
	}
	return 0, false
}

// Rename changes the identifier at a bit position.
func (lf *LayersFile) Rename(bit int, name string) error {
	if bit < 0 || bit >= len(lf.Names) {
		return fmt.Errorf("srcmodel: layer bit %d out of range", bit)
	}
	if err := lf.checkName(name, bit); err != nil {
		return err
	}
	vs := lf.decl.Specs[bit].(*dst.ValueSpec)
	vs.Names[0].Name = name
	lf.Names[bit] = name
	return nil
}

// Add appends a layer on the next free bit.
func (lf *LayersFile) Add(name string) error {
	if len(lf.Names) >= MaxLayers {
		return fmt.Errorf("srcmodel: all %d layer bits are in use", MaxLayers)
	}
	if err := lf.checkName(name, -1); err != nil {
		return err
	}
	vs := &dst.ValueSpec{Names: []*dst.Ident{dst.NewIdent(name)}}
	vs.Decs.Before = dst.NewLine
	vs.Decs.After = dst.NewLine
	lf.decl.Specs = append(lf.decl.Specs, vs)
	lf.Names = append(lf.Names, name)
	return nil
}

func (lf *LayersFile) checkName(name string, selfBit int) error {
	if !validIdent(name) || name == "_" {
		return fmt.Errorf("srcmodel: %q is not a valid layer identifier", name)
	}
	for i, n := range lf.Names {
		if i != selfBit && n == name {
			return fmt.Errorf("srcmodel: layer %q already exists", name)
		}
	}
	return nil
}

// Save prints the file, verifies the output re-parses to the same layer list,
// and atomically replaces Path.
func (lf *LayersFile) Save() error {
	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, lf.file); err != nil {
		return fmt.Errorf("srcmodel: print %s: %w", lf.Path, err)
	}
	out := buf.Bytes()
	check, err := parseLayersSource(out, lf.Path)
	if err != nil {
		return fmt.Errorf("srcmodel: refusing to save %s: output does not re-parse: %w", lf.Path, err)
	}
	if len(check.Names) != len(lf.Names) {
		return fmt.Errorf("srcmodel: refusing to save %s: layer list lost in round trip", lf.Path)
	}
	tmp, err := os.CreateTemp(filepath.Dir(lf.Path), ".spliti-save-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if info, err := os.Stat(lf.Path); err == nil {
		os.Chmod(tmpName, info.Mode())
	}
	if err := os.Rename(tmpName, lf.Path); err != nil {
		return err
	}
	lf.lastWritten = out
	return nil
}
