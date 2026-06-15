// Package editor is the spliti visual editor: a native-only app.Plugin that
// hosts the game's world inside an ImGui dockspace — scene viewport with
// picking and a transform gizmo, hierarchy, inspector with live editing — and
// writes edits back into the game's Go source (the scene file is the save
// format; see editor/srcmodel).
//
// It is never imported by a shipped game. `spliti gen` emits a generated,
// git-ignored target (.spliti/editor/main.go, //go:build !js) that imports the
// game's packages plus this plugin and runs them in one process, giving the
// editor direct ECS access. Add it after render3d.Plugin and ui.Plugin.
package editor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/gizmo"
	"github.com/hvuhsg/spliti/editor/registry"
	"github.com/hvuhsg/spliti/editor/srcmodel"
	"github.com/hvuhsg/spliti/editor/undo"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/plugin/screenshot"
	"github.com/hvuhsg/spliti/scene"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
)

// PrefabFunc is the editor-callable form of a //spliti:entity prefab.
type PrefabFunc func(c *app.Ctx, t render3d.Transform3D) ecs.Entity

// Plugin embeds the editor into an app. All fields are wired by the generated
// .spliti/editor/main.go; only Scene/SceneFile/SetupScene are required.
type Plugin struct {
	// ProjectRoot is the game module root (where spliti.toml lives).
	ProjectRoot string
	// SceneFile is the path to the scene's Go source file, relative to
	// ProjectRoot (or absolute).
	SceneFile string
	// Scene is the scene name inside SceneFile (the //spliti:scene name).
	Scene string
	// SetupScene is the compiled scene function itself.
	SetupScene app.SystemFunc
	// LoadAssets, when set, runs at startup before the scene (mesh/material
	// registration lives there by convention).
	LoadAssets app.SystemFunc
	// GameSystems, when set, is invoked during Build with a system
	// interceptor installed that gates every system it registers behind Play
	// mode (and its Systems-panel toggle). In Edit mode the world stays still.
	GameSystems func(a *app.App)
	// Registry lists the component types the inspector can edit. Nil gets
	// the engine built-ins only.
	Registry *registry.Registry
	// Prefabs maps prefab functions by their written form ("entities.SpawnCrate")
	// so the editor can spawn instances live (Assets panel, undo of delete,
	// watcher-discovered spawn lines).
	Prefabs map[string]PrefabFunc
	// GamePkg is the import path of the game's root package (the one holding
	// the //spliti:layers block). When set and named layers exist, scene.Set
	// lines write layer bits as named constants (game.LayerPlayer). Optional.
	GamePkg string
}

// state is the editor's central resource.
type state struct {
	cfg Plugin
	reg *registry.Registry

	// viewport
	rt        *render3d.RenderTarget
	texID     imgui.TextureID
	dockBuilt bool
	vpHovered bool

	// edCam is the editor's own camera, driving the Scene viewport via
	// render3d.SetSceneCamera. The global Camera3D resource stays the game's:
	// scene setup poses it, game systems drive it during Play, and the Game
	// panel renders from it.
	edCam *render3d.Camera3D

	// game view: a second scene pass (no editor gizmo lines) from the game's
	// camera into its own target, shown in the Game panel.
	gameRT    *render3d.RenderTarget
	gameView  *render3d.SceneView
	gameTexID imgui.TextureID
	// gameCamBefore restores the game camera on Stop (resources are not part
	// of the world snapshot, but the camera is engine-owned and cheap to keep).
	gameCamBefore render3d.Camera3D

	// selection: ordered, last entry is the primary (inspector/gizmo anchor)
	sel []ecs.Entity
	// dragStart captures each selected entity's local transform when a gizmo
	// drag begins, for the one-batch undo pushed at drag end.
	dragStart map[ecs.Entity]render3d.Transform3D

	// gizmo
	op gizmo.Op
	gz gizmo.Gizmo

	cam cameraRig

	// undo/redo — every committed mutation goes through here
	undo *undo.Stack

	// source model + writeback
	src         *srcmodel.SceneFile
	srcErr      error                // last parse error (scene shown read-only)
	dirty       map[string]time.Time // instance name → live-transform write deadline
	pendingSave bool
	saveAt      time.Time
	statusMsg   string
	statusAt    time.Time

	// file watcher (external edits → live world)
	watch *sceneWatcher

	// collision layers (//spliti:layers const block; optional)
	layers      *srcmodel.LayersFile
	layersErr   error
	layerEdit   map[int]string // staged rename buffers, keyed by bit
	newLayerBuf string
	// symLayers is the symbolic-layer table installed on the scene model
	// (named layer constants in scene.Set lines); nil when the project has no
	// named layers or no GamePkg.
	symLayers *srcmodel.Layers

	// input bindings (//spliti:input function; optional)
	input         *srcmodel.InputFile
	inputErr      error
	capture       captureState
	newActionBuf  string
	newActionAxis bool

	// play mode (M3): game systems run only while playing; Stop restores the
	// pre-play snapshot.
	mode               playMode
	stepPending        bool // Step clicked; arm next frame
	stepActive         bool // game systems run this frame despite Paused
	keepPlayTransforms bool // Stop re-applies played transforms as an edit
	snapshot           *worldSnapshot
	gameSystems        []*gameSystem // recorded by the registration interceptor
	reloadAfterPlay    bool          // external scene edit arrived mid-play

	// rebuild & re-exec lifecycle
	rebuildNeeded bool
	rebuild       rebuildState
	execOnExit    string // set when a successful rebuild wants a re-exec

	// export (build a shippable game artifact from the toolbar)
	export exportState

	// native macOS "File" menu item handles (zero value / inert off darwin)
	menu fileMenu

	// view holds the editor's look settings (theme, font size, grid) driven by
	// the native "View" menu, persisted in the session.
	view viewState

	// console
	console           console
	consoleAutoScroll bool

	// terminal: an interactive shell panel tabbed beside the Console. Lazily
	// started on first draw; killed on exit.
	term *terminal

	// per-widget Euler cache for quaternion fields (activation-scoped so a
	// drag never feeds back through the lossy quat→Euler conversion).
	eulerCache map[string][3]float32
	// pre-gesture component snapshots, keyed entity/type, captured when an
	// inspector widget activates and committed as one undo command when it
	// deactivates after an edit.
	editBefore map[string]any

	// assets (//spliti:assets LoadAssets function; optional). Meshes and
	// materials registered there are listed, previewed, and appended to when a
	// file is imported.
	assets    *srcmodel.AssetsFile
	assetsErr error
	// dropQueue holds OS file-drop paths captured by the glfw drop callback (on a
	// non-main goroutine), drained into importAssetFile in schedule.First.
	dropMu    sync.Mutex
	dropQueue []string

	// asset preview pane: one reusable offscreen pass showing the selected model
	// (or a material on a sphere) on a turntable. The preview entity lives far
	// from the scene so the tight preview camera frames only it.
	selAsset   string // asset key selected in the panel
	prevRT     *render3d.RenderTarget
	prevView   *render3d.SceneView
	prevTexID  imgui.TextureID
	prevEntity ecs.Entity
	prevCam    *render3d.Camera3D
	prevShown  string  // asset key currently bound to the preview entity
	prevYaw    float32 // turntable angle
	// asset 2D preview caches: image thumbnails and material swatches uploaded as
	// ImGui textures, keyed by asset key; waveforms keyed by audio asset key.
	assetTex  map[string]imgui.TextureID
	waveCache map[string]waveform
	newMatBuf string                           // "+ New Material" name buffer
	matStage  map[string]srcmodel.MaterialSpec // in-progress material edits (per key)

	// mesh thumbnails for the asset grid: each mesh is rendered once into its
	// own small offscreen target, then frozen (the view is disabled, the target
	// keeps the image). Built one at a time (thumbBuilding) off a shared
	// far-away entity/camera so each tile shows only its mesh.
	thumbs        map[string]*assetThumb
	thumbCam      *render3d.Camera3D
	thumbEntity   ecs.Entity
	thumbBuilding string

	// UI transients
	dragPrefab    string       // prefab name being dragged from the Assets panel
	dragAsset     draggedAsset // asset being dragged from the Assets panel
	dragInstance  string       // instance being dragged in the hierarchy
	renameTarget  string       // instance the rename popup edits ("" = closed)
	renameBuf     string
	addCompFilter string
	aabbMeshCache map[string][2]m.Vec3 // mesh ref → model-space AABB
}

// draggedAsset is the asset (key + kind) being dragged from the Assets panel.
type draggedAsset struct {
	key  string
	kind srcmodel.AssetKind
}

const writebackDebounce = 500 * time.Millisecond

// newState builds the editor resource with every map and subsystem ready.
func newState(p Plugin) *state {
	reg := p.Registry
	if reg == nil {
		reg = registry.New()
		registry.Builtin(reg)
	}
	p.Prefabs = withBuiltinPrefabs(p.Prefabs)
	return &state{
		cfg:               p,
		reg:               reg,
		op:                gizmo.Translate,
		view:              defaultView(),
		cam:               defaultRig(),
		undo:              undo.NewStack(0),
		dirty:             make(map[string]time.Time),
		dragStart:         make(map[ecs.Entity]render3d.Transform3D),
		layerEdit:         make(map[int]string),
		eulerCache:        make(map[string][3]float32),
		editBefore:        make(map[string]any),
		aabbMeshCache:     make(map[string][2]m.Vec3),
		assetTex:          make(map[string]imgui.TextureID),
		waveCache:         make(map[string]waveform),
		matStage:          make(map[string]srcmodel.MaterialSpec),
		consoleAutoScroll: true,
	}
}

// Build wires the editor systems around the game's own.
func (p Plugin) Build(a *app.App) {
	st := newState(p)
	app.InsertResource(a, st)

	// render3d.Build (registered first) has already created the window and its
	// Cocoa menu bar, so the native "File" menu can be installed now. Off
	// darwin this is a no-op and the commands stay in the in-app menu bar.
	st.buildNativeMenu(a.Ctx())
	st.buildViewMenu(a.Ctx())

	// Game systems register through an interceptor that records them for the
	// Systems panel and gates each behind Play mode (and its panel toggle).
	if p.GameSystems != nil {
		a.SetSystemInterceptor(st.interceptGameSystem)
		p.GameSystems(a)
		a.SetSystemInterceptor(nil)
	}

	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		io := imgui.CurrentIO()
		io.SetConfigFlags(io.ConfigFlags() | imgui.ConfigFlagsDockingEnable | imgui.ConfigFlagsNavEnableKeyboard)
		st.installLayoutPersistence(io)
		st.captureLog()
		if p.LoadAssets != nil {
			p.LoadAssets(c)
		}
		if p.SetupScene != nil {
			p.SetupScene(c)
		}
		// The editor renders through its own camera from here on; the global
		// Camera3D resource is left to the game (seeded from its startup pose).
		if gc := app.GetResource[render3d.Camera3D](c); gc != nil {
			ec := *gc
			st.edCam = &ec
			render3d.SetSceneCamera(c, st.edCam)
		}
		st.loadSceneSource()
		st.loadLayers()
		st.loadInput()
		st.loadAssetsModel()
		st.startWatcher()
		st.installDropCallback(c)
		st.restoreSession(c)
	})

	// On exit: persist the session, then chain into the rebuilt binary when a
	// rebuild requested it. Hooks run after window/terminal cleanup.
	a.AddOnExit(func() {
		st.saveSession(a.Ctx())
		if st.term != nil {
			st.term.kill()
		}
		if st.execOnExit != "" {
			execReplace(st.execOnExit)
		}
	})

	render3d.AddPreRender(a, func(c *app.Ctx) {
		st.cam.apply(st.edCam)
		if st.view.showGrid {
			drawGrid(c)
		}
		drawSelectionBox(c, st)
		drawLightIcons(c, st)
		drawCameraIcons(c, st)
		drawColliderBoxes(c)
	})
	a.AddSystems(schedule.First, drainWatcher, drainDrops, checkRebuild, checkExport, menuTick)
	a.AddSystems(schedule.Update, editorUI)
	// stepClock must run after the game's own Last systems; it is registered
	// later, and same-stage order follows insertion order.
	a.AddSystems(schedule.Last, flushWriteback, stepClock)

	installSmokeHooks(a, st)
}

// installLayoutPersistence stores the ImGui layout under .spliti/ and lets the
// saved layout win over the built-in default when one exists.
func (st *state) installLayoutPersistence(io *imgui.IO) {
	dir := filepath.Join(st.cfg.ProjectRoot, ".spliti")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	ini := filepath.Join(dir, "imgui.ini")
	io.SetIniFilename(ini)
	// A saved layout wins over the built-in default — unless it was written
	// by an editor whose default layout lacked panels added since (they would
	// float awkwardly over the dock). layoutVersion bumps on every default-
	// layout change; a mismatch rebuilds the default once.
	verFile := filepath.Join(dir, "layout.version")
	ver, _ := os.ReadFile(verFile)
	if _, err := os.Stat(ini); err == nil && string(bytes.TrimSpace(ver)) == layoutVersion {
		st.dockBuilt = true
	} else {
		os.WriteFile(verFile, []byte(layoutVersion), 0o644)
	}
}

// layoutVersion identifies the default dock layout; bump it when adding or
// removing a docked panel so stale user layouts are rebuilt once.
const layoutVersion = "6"

// installSmokeHooks wires the headless-verification env hooks (used by CI and
// scripted runs; inert otherwise): SPLITI_EDITOR_FRAMES bounds the run,
// SPLITI_EDITOR_SHOT selects the first named instance at frame 60 and saves
// screenshots at frames 90 and 180 to <prefix>.<frame>.png,
// SPLITI_EDITOR_PLAY=1 additionally starts Play at frame 70 and stops at
// frame 150 (so the screenshots bracket a full play/stop round trip), and
// SPLITI_EDITOR_REBUILD=1 triggers the rebuild-and-restart flow at frame 100
// (the variable is cleared first so the re-exec'd child runs it only once).
func installSmokeHooks(a *app.App, st *state) {
	if n, _ := strconv.Atoi(os.Getenv("SPLITI_EDITOR_FRAMES")); n > 0 {
		a.SetMaxFrames(n)
	}
	shot := os.Getenv("SPLITI_EDITOR_SHOT")
	if shot == "" {
		return
	}
	play := os.Getenv("SPLITI_EDITOR_PLAY") == "1"
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		switch c.App().Frame() {
		case 5:
			fmt.Fprintf(os.Stderr, "smoke: editor up (pid %d)\n", os.Getpid())
		case 60:
			app.Query1[scene.Name](c, func(e ecs.Entity, n *scene.Name) {
				if len(st.sel) == 0 {
					st.selectOne(e)
				}
			})
		case 70:
			if play {
				st.startPlay(c)
			}
		case 80:
			// Bring the Game tab forward so the frame-90 shot captures the
			// second scene pass (the game camera's view) mid-play.
			if play {
				imgui.SetWindowFocusStr("Game")
			}
		case 100:
			if os.Getenv("SPLITI_EDITOR_REBUILD") == "1" {
				os.Unsetenv("SPLITI_EDITOR_REBUILD")
				st.startRebuild(c)
			}
		case 150:
			if play {
				st.stopPlay(c)
			}
		case 160:
			if play {
				imgui.SetWindowFocusStr("Scene")
			}
		case 90, 180:
			screenshot.Save(c, fmt.Sprintf("%s.%d.png", shot, c.App().Frame()))
		}
	})
}

// editorUI is the per-frame ImGui pass: dock shell, panels, shortcuts.
func editorUI(c *app.Ctx) {
	st := app.GetResource[state](c)
	st.pruneSelection(c)
	st.handleShortcuts(c)
	st.view.apply()
	nColors, nVars := pushEditorTheme(st.view.theme())
	drawShell(c, st)
	drawHierarchy(c, st)
	drawInspector(c, st)
	drawAssets(c, st)
	drawSystems(c, st)
	drawLayers(c, st)
	drawInput(c, st)
	drawConsole(c, st)
	drawTerminal(c, st)
	drawViewport(c, st)
	drawGameView(c, st)
	imgui.PopStyleColorV(nColors)
	imgui.PopStyleVarV(nVars)
}

func (st *state) handleShortcuts(c *app.Ctx) {
	// While the Terminal panel has focus its keystrokes belong to the shell, not
	// the editor's gizmo/save/undo shortcuts. (Reads last frame's focus; the lag
	// is harmless.)
	if st.term != nil && st.term.focused {
		return
	}
	io := imgui.CurrentIO()
	// Gate on WantTextInput, not WantCaptureKeyboard: with NavEnableKeyboard
	// set, the latter is true whenever any window (including the focused Scene
	// viewport) has focus, which would swallow W/E/R and the other shortcuts.
	// WantTextInput is true only while a text field is being edited.
	if io.WantTextInput() {
		return
	}
	ctrl := io.KeyCtrl() || io.KeySuper()
	shift := io.KeyShift()
	switch {
	case ctrl && imgui.IsKeyPressedBool(imgui.KeyP):
		if st.mode == modeEdit {
			st.startPlay(c)
		} else {
			st.stopPlay(c)
		}
	case ctrl && shift && imgui.IsKeyPressedBool(imgui.KeyZ):
		st.redo(c)
	case ctrl && imgui.IsKeyPressedBool(imgui.KeyZ):
		st.undoLast(c)
	case ctrl && imgui.IsKeyPressedBool(imgui.KeyY):
		st.redo(c)
	case ctrl && imgui.IsKeyPressedBool(imgui.KeyS):
		st.saveNow(c, true)
	case ctrl && imgui.IsKeyPressedBool(imgui.KeyD):
		st.duplicateSelection(c)
	case ctrl && (imgui.IsKeyPressedBool(imgui.KeyEqual) || imgui.IsKeyPressedBool(imgui.KeyKeypadAdd)):
		st.view.zoomFont(fontScaleStep)
	case ctrl && (imgui.IsKeyPressedBool(imgui.KeyMinus) || imgui.IsKeyPressedBool(imgui.KeyKeypadSubtract)):
		st.view.zoomFont(-fontScaleStep)
	case ctrl && imgui.IsKeyPressedBool(imgui.Key0):
		st.view.resetFont()
	case imgui.IsKeyPressedBool(imgui.KeyDelete) || imgui.IsKeyPressedBool(imgui.KeyBackspace):
		st.deleteSelection(c)
	case imgui.IsKeyPressedBool(imgui.KeyW):
		st.op = gizmo.Translate
	case imgui.IsKeyPressedBool(imgui.KeyE):
		st.op = gizmo.Rotate
	case imgui.IsKeyPressedBool(imgui.KeyR):
		st.op = gizmo.Scale
	case imgui.IsKeyPressedBool(imgui.KeyF):
		st.cam.focusSelection(c, st)
	}
}

func (st *state) undoLast(c *app.Ctx) {
	if st.mode != modeEdit {
		st.status("undo is disabled during play")
		return
	}
	name, err := st.undo.Undo(c)
	switch {
	case err != nil:
		st.status(fmt.Sprintf("undo %s: %v", name, err))
	case name != "":
		st.status("undid " + name)
		st.touchSource()
	}
}

func (st *state) redo(c *app.Ctx) {
	if st.mode != modeEdit {
		st.status("redo is disabled during play")
		return
	}
	name, err := st.undo.Redo(c)
	switch {
	case err != nil:
		st.status(fmt.Sprintf("redo %s: %v", name, err))
	case name != "":
		st.status("redid " + name)
		st.touchSource()
	}
}

func (st *state) duplicateSelection(c *app.Ctx) {
	var cmds []editorCmd
	var names []string
	for _, e := range st.sel {
		src := instanceName(c, e)
		if src == "" {
			continue
		}
		name := st.freeInstanceName(c, src)
		cmds = append(cmds, &cmdDuplicate{src: src, instance: name})
		names = append(names, name)
	}
	if len(cmds) == 0 {
		if len(st.sel) > 0 {
			st.status("cannot duplicate unnamed entities")
		}
		return
	}
	st.push(c, batch("duplicate", cmds))
	st.clearSelection()
	for _, name := range names {
		if e, ok := entityByInstance(c, name); ok {
			st.sel = append(st.sel, e)
		}
	}
}

func (st *state) deleteSelection(c *app.Ctx) {
	if len(st.sel) == 0 {
		return
	}
	// Skip entities whose selected ancestor already takes them along, so the
	// batch never deletes the same subtree twice.
	var cmds []editorCmd
	for _, e := range st.sel {
		covered := false
		for _, other := range st.sel {
			if other != e && isDescendant(c, e, other) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		inst := instanceName(c, e)
		if inst == "" {
			despawnSubtree(c, e)
			continue
		}
		cmds = append(cmds, &cmdDelete{root: inst})
	}
	st.clearSelection()
	if len(cmds) > 0 {
		st.push(c, batch("delete", cmds))
	}
}

// freeInstanceName derives an unused instance name from a base name. The live
// and source name sets are gathered once up front so each candidate is an O(1)
// map probe rather than a fresh world scan plus source scan per try.
func (st *state) freeInstanceName(c *app.Ctx, base string) string {
	live := buildNameIndex(c)
	inSource := map[string]bool{}
	if sc := st.scene(); sc != nil {
		for _, sp := range sc.Spawns {
			inSource[sp.Instance] = true
		}
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s%d", base, i)
		if _, exists := live[name]; exists {
			continue
		}
		if inSource[name] {
			continue
		}
		return name
	}
}

// instanceName returns the scene.Name of e, or "" when it has none (entities
// without a name are live-only: editable in the session, never written back).
func instanceName(c *app.Ctx, e ecs.Entity) string {
	w := c.World()
	if !w.Alive(e) {
		return ""
	}
	mp := nameMap(c)
	if !mp.Has(e) {
		return ""
	}
	return mp.Get(e).Value
}

func (st *state) status(msg string) {
	st.statusMsg = msg
	st.statusAt = time.Now()
}

// markTransformDirty schedules a debounced source write for the instance (the
// live-scrub path; committed gestures go through cmdTransform instead).
func (st *state) markTransformDirty(instance string) {
	if instance == "" || st.src == nil || st.mode != modeEdit {
		return
	}
	st.dirty[instance] = time.Now().Add(writebackDebounce)
}

// touchSource schedules a debounced save of the (already mutated) scene model.
func (st *state) touchSource() {
	if st.src == nil || st.srcErr != nil || st.mode != modeEdit {
		return
	}
	st.pendingSave = true
	st.saveAt = time.Now().Add(writebackDebounce)
}

// unsaved reports whether the model holds changes not yet on disk.
func (st *state) unsaved() bool { return st.pendingSave || len(st.dirty) > 0 }

// vec2 converts an imgui vec for the gizmo frame.
func vec2(v imgui.Vec2) m.Vec2 { return m.Vec2{X: v.X, Y: v.Y} }
