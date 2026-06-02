package project

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// ProjectFile is the on-disk root document for an editor project. It lives
// at <projectDir>/project.toml. Sprites and scenes are NOT enumerated here
// — the loader walks the project's sprites/ and scenes/ directories — so
// adding a new asset is just dropping a file in the right folder.
type ProjectFile struct {
	Name         string           `toml:"name"`
	Version      string           `toml:"version,omitempty"`
	DefaultScene string           `toml:"defaultScene"`
	Settings     ProjectSettings  `toml:"settings,omitempty"`
}

// ProjectSettings carries runtime knobs the editor reads at load time.
// Viewport sizes the play-area inside the editor's center panel; FixedHz
// drives how fast game ticks happen in Play mode.
type ProjectSettings struct {
	FixedHz  int           `toml:"fixedHz,omitempty"`
	Viewport ViewportPrefs `toml:"viewport,omitempty"`
}

// ViewportPrefs are the dimensions a project wants its play-area to have.
// The editor still subjects them to its own panel layout — these are
// preferences, not guarantees.
type ViewportPrefs struct {
	W int `toml:"w,omitempty"`
	H int `toml:"h,omitempty"`
}

// Project is an in-memory snapshot of a loaded project: the file, the
// project directory, and lists of sprite/scene paths the editor uses for
// browsing.
//
// We don't auto-load every sprite or scene at Project.Load time — the
// editor lazily loads sprites into the SpriteRegistry when they're first
// needed and lazily parses scene files when one is opened.
type Project struct {
	Dir         string
	File        *ProjectFile
	SpritePaths []string // absolute, sorted
	ScenePaths  []string // absolute, sorted
}

// LoadProject reads <dir>/project.toml and walks sprites/ + scenes/.
// Missing sprite or scene directories are tolerated (a fresh project
// might have neither yet).
func LoadProject(dir string) (*Project, error) {
	pfPath := filepath.Join(dir, "project.toml")
	data, err := os.ReadFile(pfPath)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", pfPath, err)
	}
	var pf ProjectFile
	if _, err := toml.Decode(string(data), &pf); err != nil {
		return nil, fmt.Errorf("parse %q: %w", pfPath, err)
	}
	p := &Project{Dir: dir, File: &pf}
	p.SpritePaths, err = listFiles(filepath.Join(dir, "sprites"), ".sprite")
	if err != nil {
		return nil, err
	}
	p.ScenePaths, err = listFiles(filepath.Join(dir, "scenes"), ".scene")
	if err != nil {
		return nil, err
	}
	return p, nil
}

// SaveProject writes <dir>/project.toml. Sprites and scenes are saved
// individually through SaveSpriteFile / SaveSceneFile.
func SaveProject(p *Project) error {
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = "  "
	if err := enc.Encode(p.File); err != nil {
		return fmt.Errorf("encode project: %w", err)
	}
	tmp := filepath.Join(p.Dir, "project.toml.tmp")
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(p.Dir, "project.toml"))
}

// NewEmptyProject creates a fresh project skeleton at dir, including the
// project.toml file and empty sprites/ and scenes/ subdirectories. Used by
// the editor's "New Project" command.
func NewEmptyProject(dir, name string) (*Project, error) {
	if err := os.MkdirAll(filepath.Join(dir, "sprites"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		return nil, err
	}
	p := &Project{
		Dir: dir,
		File: &ProjectFile{
			Name:         name,
			Version:      "0.1",
			DefaultScene: "main",
			Settings: ProjectSettings{
				FixedHz:  60,
				Viewport: ViewportPrefs{W: 60, H: 22},
			},
		},
	}
	if err := SaveProject(p); err != nil {
		return nil, err
	}
	return p, nil
}

// ScenePathByName resolves a scene name to its file path. Returns ""
// if no scene with that name exists in the project.
func (p *Project) ScenePathByName(name string) string {
	want := name + ".scene"
	for _, sp := range p.ScenePaths {
		if filepath.Base(sp) == want {
			return sp
		}
	}
	return ""
}

// SpritePathByRef resolves a sprite ref to its file path by filename
// stem. Returns "" if not present. The editor also checks the in-memory
// SpriteRegistry separately; this is for "find on disk" needs.
func (p *Project) SpritePathByRef(ref string) string {
	want := ref + ".sprite"
	for _, sp := range p.SpritePaths {
		if filepath.Base(sp) == want {
			return sp
		}
	}
	return ""
}

// listFiles returns sorted absolute paths in dir matching ext. A
// non-existent dir returns an empty slice (not an error) so a half-built
// project still loads.
func listFiles(dir, ext string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}
