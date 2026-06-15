package editor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/srcmodel"
	"github.com/hvuhsg/spliti/plugin/render3d"
)

// prefabDragType tags ImGui drag payloads of a prefab dragged from the Assets
// panel; the dragged prefab name lives in state.dragPrefab.
const prefabDragType = "spliti-prefab"

// assetDragType tags ImGui drag payloads of an asset (mesh/material/image/audio)
// dragged from the Assets panel; the asset's key+kind lives in state.dragAsset.
const assetDragType = "spliti-asset"

// Editor-local asset kinds for files the engine previews but does not register
// in code (no standalone texture registry; the audio plugin may be absent).
const (
	assetKindImage srcmodel.AssetKind = "image"
	assetKindAudio srcmodel.AssetKind = "audio"
)

// reservedAssetPrefix marks editor-internal registry entries (the material
// preview sphere) that should never appear in asset lists or inspector pickers.
const reservedAssetPrefix = "__"

// drawAssets is the asset browser: the project's prefabs (drag into the scene),
// its registered meshes and materials (//spliti:assets), and the image/audio
// files in game/assets (preview only). Drop a file from the OS onto the window
// to import it; drag a model into the scene to spawn it, or onto a component's
// Mesh/Material field to set it.
func drawAssets(c *app.Ctx, st *state) {
	open := imgui.Begin("Assets")
	defer imgui.End()
	if !open {
		// Panel hidden/collapsed: stop the offscreen preview pass.
		st.disablePreview()
		return
	}

	imgui.TextDisabled("drop files here to import (.obj, .png/.jpg, .wav/.mp3/.ogg)")
	imgui.Separator()

	drawPrefabSection(c, st)
	drawModelSection(c, st)
	drawMaterialSection(c, st)
	drawImageSection(c, st)
	drawAudioSection(c, st)

	imgui.Separator()
	st.drawAssetPreview(c)
}

// drawPrefabSection lists //spliti:entity prefabs; drag one into the scene.
func drawPrefabSection(c *app.Ctx, st *state) {
	if !imgui.CollapsingHeaderBoolPtrV("Prefabs", nil, imgui.TreeNodeFlagsDefaultOpen) {
		return
	}
	names := make([]string, 0, len(st.cfg.Prefabs))
	for name := range st.cfg.Prefabs {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		imgui.TextDisabled("no prefabs (game/entities)")
		return
	}
	for _, name := range names {
		label := prefabLabel(name)
		imgui.PushIDStr(name) // labels can collide (two SpawnX in different pkgs)
		imgui.SelectableBool(label)
		if imgui.BeginDragDropSource() {
			st.dragPrefab = name
			imgui.SetDragDropPayload(prefabDragType, dummyPayload(), 1)
			imgui.TextUnformatted(label)
			imgui.EndDragDropSource()
		}
		if imgui.IsItemHovered() && imgui.IsMouseDoubleClicked(imgui.MouseButtonLeft) {
			st.spawnPrefab(c, name, spawnPointFallback)
		}
		imgui.PopID()
	}
}

// drawModelSection shows registered meshes as a thumbnail grid; drag one into
// the scene to spawn it, or double-click to spawn at the origin.
func drawModelSection(c *app.Ctx, st *state) {
	if !imgui.CollapsingHeaderBoolPtrV("Models", nil, imgui.TreeNodeFlagsDefaultOpen) {
		return
	}
	meshes := app.GetResource[render3d.MeshRegistry](c)
	if meshes == nil {
		return
	}
	var items []assetItem
	for _, key := range meshes.Keys() {
		if strings.HasPrefix(key, reservedAssetPrefix) {
			continue
		}
		items = append(items, assetItem{
			key: key, label: key, kind: srcmodel.AssetMesh,
			tex: st.thumbFor(c, key), spawnMesh: true, tip: key + modelTag(st, key),
		})
	}
	if len(items) == 0 {
		imgui.TextDisabled("no meshes registered")
		return
	}
	st.assetGrid(c, items)
}

// modelTag annotates a mesh tooltip with its source (file or procedural).
func modelTag(st *state, key string) string {
	if st.assets == nil {
		return ""
	}
	if e := st.assets.Entry(key); e != nil {
		if e.Procedural {
			return "  (procedural)"
		}
		if e.File != "" {
			return "  " + e.File
		}
	}
	return ""
}

// drawMaterialSection shows registered materials as color-swatch tiles; drag one
// onto a MaterialRef field. "+ New Material" appends one to LoadAssets.
func drawMaterialSection(c *app.Ctx, st *state) {
	if !imgui.CollapsingHeaderBoolPtrV("Materials", nil, imgui.TreeNodeFlagsDefaultOpen) {
		return
	}
	if materials := app.GetResource[render3d.MaterialRegistry](c); materials != nil {
		var items []assetItem
		for _, key := range materials.Keys() {
			if strings.HasPrefix(key, reservedAssetPrefix) {
				continue
			}
			sw := st.materialColor(key)
			items = append(items, assetItem{key: key, label: key, kind: srcmodel.AssetMaterial, swatch: &sw})
		}
		st.assetGrid(c, items)
	}
	st.drawNewMaterial(c)
}

// drawImageSection shows image files in game/assets as thumbnail tiles.
func drawImageSection(c *app.Ctx, st *state) {
	imgs := filterLoose(st.scanLooseAssets(), classImage)
	if len(imgs) == 0 {
		return
	}
	if !imgui.CollapsingHeaderBoolPtr("Images", nil) {
		return
	}
	var items []assetItem
	for _, a := range imgs {
		items = append(items, assetItem{
			key: a.file, label: a.key, kind: assetKindImage, tex: st.imageTexFor(c, a.file, a.file),
		})
	}
	st.assetGrid(c, items)
}

// drawAudioSection shows audio files in game/assets as tiles; select one to see
// its waveform and play it in the preview pane below.
func drawAudioSection(c *app.Ctx, st *state) {
	clips := filterLoose(st.scanLooseAssets(), classAudio)
	if len(clips) == 0 {
		return
	}
	if !imgui.CollapsingHeaderBoolPtr("Audio", nil) {
		return
	}
	var items []assetItem
	for _, a := range clips {
		items = append(items, assetItem{key: a.file, label: a.key, kind: assetKindAudio})
	}
	st.assetGrid(c, items)
}

// assetItem is one tile in the asset grid.
type assetItem struct {
	key, label string
	kind       srcmodel.AssetKind
	tex        imgui.TextureID // thumbnail/image texture, or 0 for a placeholder
	swatch     *imgui.Vec4     // solid color tile (materials), or nil
	spawnMesh  bool            // double-click / drop spawns this mesh
	tip        string          // tooltip override (defaults to label)
}

// assetTileSize is the side length of a grid tile's picture, in logical px.
const assetTileSize = 84

// assetGrid lays items out left-to-right, wrapping to fill the panel width.
func (st *state) assetGrid(c *app.Ctx, items []assetItem) {
	if len(items) == 0 {
		return
	}
	cols := int(imgui.ContentRegionAvail().X / (assetTileSize + 16))
	if cols < 1 {
		cols = 1
	}
	for i := range items {
		st.assetTile(c, &items[i])
		if (i+1)%cols != 0 && i+1 < len(items) {
			imgui.SameLine()
		}
	}
}

// assetTile draws one grid tile: a picture button (thumbnail, swatch, or text
// placeholder) plus a truncated label. The tile selects on click, is a drag
// source for the asset, double-click-spawns meshes, and outlines when selected.
// PushID keeps tiles with identical labels (e.g. a mesh and material both named
// "marker") from colliding in ImGui's ID stack.
func (st *state) assetTile(c *app.Ctx, it *assetItem) {
	imgui.PushIDStr(string(it.kind) + "/" + it.key)
	defer imgui.PopID()
	imgui.BeginGroup()
	size := imgui.Vec2{X: assetTileSize, Y: assetTileSize}
	switch {
	case it.tex != 0:
		if imgui.ImageButtonV("##t", *imgui.NewTextureRefTextureID(it.tex), size,
			imgui.Vec2{}, imgui.Vec2{X: 1, Y: 1}, imgui.Vec4{}, imgui.Vec4{X: 1, Y: 1, Z: 1, W: 1}) {
			st.selAsset = it.key
		}
	case it.swatch != nil:
		if imgui.ColorButtonV("##t", *it.swatch, imgui.ColorEditFlagsNoTooltip, size) {
			st.selAsset = it.key
		}
	default:
		if imgui.ButtonV(tilePlaceholder(it.kind), size) {
			st.selAsset = it.key
		}
	}
	if it.spawnMesh && imgui.IsItemHovered() && imgui.IsMouseDoubleClicked(imgui.MouseButtonLeft) {
		st.spawnMeshAsset(c, it.key, spawnPointFallback)
	}
	if imgui.BeginDragDropSource() {
		st.dragAsset = draggedAsset{key: it.key, kind: it.kind}
		imgui.SetDragDropPayload(assetDragType, dummyPayload(), 1)
		imgui.TextUnformatted(it.label)
		imgui.EndDragDropSource()
	}
	tip := it.label
	if it.tip != "" {
		tip = it.tip
	}
	imgui.SetItemTooltip(tip)
	imgui.TextUnformatted(truncLabel(it.label, 11))
	imgui.EndGroup()
	if st.selAsset == it.key {
		imgui.WindowDrawList().AddRectV(imgui.ItemRectMin(), imgui.ItemRectMax(),
			assetAccentU32(), 4, 0, 2)
	}
}

// tilePlaceholder is the glyph shown on a tile with no picture (a mesh whose
// thumbnail is still rendering, or an audio clip).
func tilePlaceholder(kind srcmodel.AssetKind) string {
	switch kind {
	case assetKindAudio:
		return "♪" // ♪
	case srcmodel.AssetMesh:
		return "..."
	default:
		return "?"
	}
}

// truncLabel shortens a label to max runes with an ellipsis.
func truncLabel(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// assetAccentU32 is the selection-outline color.
func assetAccentU32() uint32 {
	return imgui.ColorConvertFloat4ToU32(imgui.Vec4{X: 0.3, Y: 0.8, Z: 0.8, W: 1})
}

// filterLoose returns the loose assets of a given class.
func filterLoose(in []looseAsset, cls assetClass) []looseAsset {
	var out []looseAsset
	for _, a := range in {
		if a.cls == cls {
			out = append(out, a)
		}
	}
	return out
}

// drawNewMaterial is the "+ New Material" affordance: a name field and a button
// that registers a neutral grey material.
func (st *state) drawNewMaterial(c *app.Ctx) {
	imgui.SetNextItemWidth(140)
	imgui.InputTextWithHint("##newmat", "new material name", &st.newMatBuf, 0, nil)
	imgui.SameLineV(0, 6)
	if imgui.Button("+ New Material") && st.newMatBuf != "" {
		key := baseKey(st.newMatBuf)
		if st.assets != nil && st.assets.Has(key) {
			st.status(fmt.Sprintf("material %q already exists", key))
		} else {
			st.push(c, &cmdNewMaterial{key: key, spec: srcmodel.MaterialSpec{R: 0.7, G: 0.7, B: 0.72, A: 1, Roughness: 0.6}})
			st.selAsset = key
			st.newMatBuf = ""
		}
	}
}
