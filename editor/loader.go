package editor

import (
	"fmt"

	"github.com/hvuhsg/spliti/editor/project"
	"github.com/hvuhsg/spliti/plugin/sprite"
)

// LoadProjectAssets loads every sprite asset in the project into the
// SpriteRegistry. Scenes are loaded on demand (LoadScene below) because
// only one scene is active at a time; sprites are referenced by many
// scenes so it's worth eagerly loading them all.
//
// Returns the count of loaded sprites and the first error encountered.
// Per-sprite errors abort the load — a malformed asset is louder than
// a silently-missing one.
func LoadProjectAssets(p *project.Project, registry *sprite.SpriteRegistry) (int, error) {
	for _, path := range p.SpritePaths {
		sf, err := project.LoadSpriteFile(path)
		if err != nil {
			return 0, fmt.Errorf("load sprite %s: %w", path, err)
		}
		asset, err := project.AssetFromFile(sf)
		if err != nil {
			return 0, fmt.Errorf("decode sprite %s: %w", path, err)
		}
		registry.Set(sf.Ref, asset)
	}
	return len(p.SpritePaths), nil
}
