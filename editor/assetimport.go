package editor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/srcmodel"
	"github.com/hvuhsg/spliti/plugin/render3d"
)

// Asset import: dragging a file onto the editor window (drop.go) or the Assets
// panel routes here. A model (.obj) is copied into game/assets/, loaded live so
// it previews and spawns immediately, and registered in LoadAssets via the
// //spliti:assets src-model (so the built game picks it up on the next rebuild).
// Images and audio are copied for preview only — render3d has no standalone
// texture registry and the audio plugin may not be present, so they are not
// auto-registered in code.

type assetClass int

const (
	classUnknown assetClass = iota
	classMesh               // .obj — registered as a mesh
	classModel              // .gltf/.glb — copied only (no single-mesh key)
	classImage              // .png/.jpg — preview only
	classAudio              // .wav/.mp3/.ogg — preview + play only
)

// classify maps a file extension to its asset class.
func classify(path string) assetClass {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".obj":
		return classMesh
	case ".gltf", ".glb":
		return classModel
	case ".png", ".jpg", ".jpeg":
		return classImage
	case ".wav", ".mp3", ".ogg":
		return classAudio
	default:
		return classUnknown
	}
}

// assetsDir is the project's asset directory (game/assets by convention).
func (st *state) assetsDir() string {
	return filepath.Join(st.cfg.ProjectRoot, "game", "assets")
}

// loadAssetsModel locates and parses the //spliti:assets function; optional.
func (st *state) loadAssetsModel() {
	path, ok := srcmodel.FindAssetsFile(filepath.Join(st.cfg.ProjectRoot, "game"))
	if !ok {
		st.assets, st.assetsErr = nil, nil
		return
	}
	st.assets, st.assetsErr = srcmodel.ParseAssetsFile(path)
}

// saveAssetsModel persists the assets file and raises the rebuild banner: the
// running game won't see the new registration until it is rebuilt (the editor
// has already loaded it live).
func (st *state) saveAssetsModel() {
	if st.assets == nil {
		return
	}
	if err := st.assets.Save(); err != nil {
		st.status(err.Error())
		return
	}
	if !st.rebuildNeeded {
		st.rebuildNeeded = true
		st.logf(logWarn, "assets changed - rebuild needed for the game to use them")
	}
}

// importAssetFile classifies srcPath, copies it into game/assets when needed,
// and registers/loads it according to its kind.
func (st *state) importAssetFile(c *app.Ctx, srcPath string) {
	cls := classify(srcPath)
	if cls == classUnknown {
		st.status(fmt.Sprintf("ignored %s (unsupported type)", filepath.Base(srcPath)))
		return
	}
	rel, abs, err := st.ensureInAssets(srcPath)
	if err != nil {
		st.status(err.Error())
		return
	}
	switch cls {
	case classMesh:
		key := st.freeAssetKey(c, baseKey(srcPath))
		st.push(c, &cmdImportMesh{key: key, file: rel, abs: abs})
	case classImage, classAudio:
		delete(st.assetTex, baseKey(srcPath)) // refresh any stale preview
		st.status(fmt.Sprintf("imported %s (preview only — not auto-registered)", filepath.Base(rel)))
	case classModel:
		st.status(fmt.Sprintf("copied %s — glTF auto-registration isn't supported yet; register it in LoadAssets", filepath.Base(rel)))
	}
}

// ensureInAssets returns the project-relative and absolute paths for srcPath
// inside game/assets, copying the file in when it lives elsewhere. A name clash
// with a different file is resolved by suffixing the basename.
func (st *state) ensureInAssets(srcPath string) (rel, abs string, err error) {
	dir := st.assetsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("assets dir: %w", err)
	}
	abs, _ = filepath.Abs(srcPath)
	if d, _ := filepath.Abs(dir); strings.HasPrefix(abs, d+string(os.PathSeparator)) {
		// Already inside game/assets — register in place.
		rel, _ = filepath.Rel(st.cfg.ProjectRoot, abs)
		return filepath.ToSlash(rel), abs, nil
	}
	name := filepath.Base(srcPath)
	dest := filepath.Join(dir, name)
	for i := 2; fileExists(dest); i++ {
		ext := filepath.Ext(name)
		dest = filepath.Join(dir, fmt.Sprintf("%s_%d%s", strings.TrimSuffix(name, ext), i, ext))
	}
	if err := copyFile(srcPath, dest); err != nil {
		return "", "", fmt.Errorf("copy %s: %w", name, err)
	}
	rel, _ = filepath.Rel(st.cfg.ProjectRoot, dest)
	return filepath.ToSlash(rel), dest, nil
}

// baseKey derives an asset key from a file name: the stem, sanitized to a valid
// Go-identifier-ish ref ([A-Za-z0-9_], leading letter/underscore).
func baseKey(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var b strings.Builder
	for i, r := range stem {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			b.WriteRune(r)
		case r >= '0' && r <= '9' && i > 0:
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	key := b.String()
	if key == "" || (key[0] >= '0' && key[0] <= '9') {
		key = "asset_" + key
	}
	return key
}

// freeAssetKey returns base, or base_2/base_3/... when it collides with an
// existing registered asset key or mesh registry ref.
func (st *state) freeAssetKey(c *app.Ctx, base string) string {
	taken := func(k string) bool {
		if st.assets != nil && st.assets.Has(k) {
			return true
		}
		if r := app.GetResource[render3d.MeshRegistry](c); r != nil {
			for _, ex := range r.Keys() {
				if ex == k {
					return true
				}
			}
		}
		return false
	}
	if !taken(base) {
		return base
	}
	for i := 2; ; i++ {
		k := fmt.Sprintf("%s_%d", base, i)
		if !taken(k) {
			return k
		}
	}
}

// looseAsset is an image/audio file present in game/assets that the panel
// previews but the engine does not register in code.
type looseAsset struct {
	key  string
	file string // project-relative path
	cls  assetClass
}

// scanLooseAssets lists image and audio files in game/assets, sorted by name.
func (st *state) scanLooseAssets() []looseAsset {
	entries, err := os.ReadDir(st.assetsDir())
	if err != nil {
		return nil
	}
	var out []looseAsset
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		cls := classify(e.Name())
		if cls != classImage && cls != classAudio {
			continue
		}
		out = append(out, looseAsset{
			key:  e.Name(),
			file: filepath.ToSlash(filepath.Join("game", "assets", e.Name())),
			cls:  cls,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
