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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	// mode. Until Play mode lands (M3), gated systems never run — the world
	// stays still for editing.
	GameSystems func(a *app.App)
	// Registry lists the component types the inspector can edit. Nil gets
	// the engine built-ins only.
	Registry *registry.Registry
	// Prefabs maps prefab functions by their written form ("entities.SpawnCrate")
	// so the editor can spawn instances live (Assets panel, undo of delete,
	// watcher-discovered spawn lines).
	Prefabs map[string]PrefabFunc
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

	// selection
	selected    ecs.Entity
	hasSelected bool

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

	// per-widget Euler cache for quaternion fields (activation-scoped so a
	// drag never feeds back through the lossy quat→Euler conversion).
	eulerCache map[string][3]float32
	// pre-gesture component snapshots, keyed entity/type, captured when an
	// inspector widget activates and committed as one undo command when it
	// deactivates after an edit.
	editBefore map[string]any

	// UI transients
	dragPrefab    string // prefab name being dragged from the Assets panel
	dragInstance  string // instance being dragged in the hierarchy
	renameTarget  string // instance the rename popup edits ("" = closed)
	renameBuf     string
	addCompFilter string
	aabbMeshCache map[string][2]m.Vec3 // mesh ref → model-space AABB
}

const writebackDebounce = 500 * time.Millisecond

// newState builds the editor resource with every map and subsystem ready.
func newState(p Plugin) *state {
	reg := p.Registry
	if reg == nil {
		reg = registry.New()
		registry.Builtin(reg)
	}
	return &state{
		cfg:           p,
		reg:           reg,
		op:            gizmo.Translate,
		cam:           defaultRig(),
		undo:          undo.NewStack(0),
		dirty:         make(map[string]time.Time),
		eulerCache:    make(map[string][3]float32),
		editBefore:    make(map[string]any),
		aabbMeshCache: make(map[string][2]m.Vec3),
	}
}

// Build wires the editor systems around the game's own.
func (p Plugin) Build(a *app.App) {
	st := newState(p)
	app.InsertResource(a, st)

	// Game systems register through an interceptor that parks them behind a
	// run-condition that is false until Play mode exists (M3). Their labels
	// and ordering are preserved for that day.
	if p.GameSystems != nil {
		a.SetSystemInterceptor(func(stage schedule.Stage, cfg *app.SystemConfig) *app.SystemConfig {
			switch stage {
			case schedule.Startup, schedule.PreStartup, schedule.PostStartup:
				return cfg // asset loading and other one-shot setup still runs
			}
			return cfg.RunIf(func(*app.Ctx) bool { return false })
		})
		p.GameSystems(a)
		a.SetSystemInterceptor(nil)
	}

	a.AddSystems(schedule.Startup, func(c *app.Ctx) {
		io := imgui.CurrentIO()
		io.SetConfigFlags(io.ConfigFlags() | imgui.ConfigFlagsDockingEnable | imgui.ConfigFlagsNavEnableKeyboard)
		st.installLayoutPersistence(io)
		if p.LoadAssets != nil {
			p.LoadAssets(c)
		}
		if p.SetupScene != nil {
			p.SetupScene(c)
		}
		st.loadSceneSource()
		st.startWatcher()
	})

	render3d.AddPreRender(a, func(c *app.Ctx) {
		st.cam.apply(c)
		drawGrid(c)
		drawSelectionBox(c, st)
	})
	a.AddSystems(schedule.First, drainWatcher)
	a.AddSystems(schedule.Update, editorUI)
	a.AddSystems(schedule.Last, flushWriteback)

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
	if _, err := os.Stat(ini); err == nil {
		st.dockBuilt = true // a saved layout exists; skip the default DockBuilder pass
	}
}

// installSmokeHooks wires the headless-verification env hooks (used by CI and
// scripted runs; inert otherwise): SPLITI_EDITOR_FRAMES bounds the run,
// SPLITI_EDITOR_SHOT selects the first named instance at frame 60 and saves
// screenshots at frames 90 and 180 to <prefix>.<frame>.png.
func installSmokeHooks(a *app.App, st *state) {
	if n, _ := strconv.Atoi(os.Getenv("SPLITI_EDITOR_FRAMES")); n > 0 {
		a.SetMaxFrames(n)
	}
	shot := os.Getenv("SPLITI_EDITOR_SHOT")
	if shot == "" {
		return
	}
	a.AddSystems(schedule.Update, func(c *app.Ctx) {
		switch c.App().Frame() {
		case 60:
			app.Query1[scene.Name](c, func(e ecs.Entity, n *scene.Name) {
				if !st.hasSelected {
					st.selected, st.hasSelected = e, true
				}
			})
		case 90, 180:
			screenshot.Save(c, fmt.Sprintf("%s.%d.png", shot, c.App().Frame()))
		}
	})
}

// editorUI is the per-frame ImGui pass: dock shell, panels, shortcuts.
func editorUI(c *app.Ctx) {
	st := app.GetResource[state](c)
	st.handleShortcuts(c)
	drawShell(c, st)
	drawHierarchy(c, st)
	drawInspector(c, st)
	drawAssets(c, st)
	drawViewport(c, st)
}

func (st *state) handleShortcuts(c *app.Ctx) {
	io := imgui.CurrentIO()
	if io.WantCaptureKeyboard() {
		return
	}
	ctrl := io.KeyCtrl() || io.KeySuper()
	shift := io.KeyShift()
	switch {
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
	if !st.hasSelected {
		return
	}
	src := instanceName(c, st.selected)
	if src == "" {
		st.status("cannot duplicate an unnamed entity")
		return
	}
	cmd := &cmdDuplicate{src: src, instance: st.freeInstanceName(c, src)}
	st.push(c, cmd)
	if e, ok := entityByInstance(c, cmd.instance); ok {
		st.selected, st.hasSelected = e, true
	}
}

func (st *state) deleteSelection(c *app.Ctx) {
	if !st.hasSelected {
		return
	}
	inst := instanceName(c, st.selected)
	if inst == "" {
		despawnSubtree(c, st.selected)
		st.hasSelected = false
		return
	}
	st.push(c, &cmdDelete{root: inst})
}

// freeInstanceName derives an unused instance name from a base name.
func (st *state) freeInstanceName(c *app.Ctx, base string) string {
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s%d", base, i)
		if _, exists := entityByInstance(c, name); exists {
			continue
		}
		if sc := st.scene(); sc != nil && sc.Spawn(name) != nil {
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
	if instance == "" || st.src == nil {
		return
	}
	st.dirty[instance] = time.Now().Add(writebackDebounce)
}

// touchSource schedules a debounced save of the (already mutated) scene model.
func (st *state) touchSource() {
	if st.src == nil || st.srcErr != nil {
		return
	}
	st.pendingSave = true
	st.saveAt = time.Now().Add(writebackDebounce)
}

// unsaved reports whether the model holds changes not yet on disk.
func (st *state) unsaved() bool { return st.pendingSave || len(st.dirty) > 0 }

// vec2 converts an imgui vec for the gizmo frame.
func vec2(v imgui.Vec2) m.Vec2 { return m.Vec2{X: v.X, Y: v.Y} }
