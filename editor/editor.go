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
	"strconv"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/editor/gizmo"
	"github.com/hvuhsg/spliti/editor/registry"
	"github.com/hvuhsg/spliti/editor/srcmodel"
	"github.com/hvuhsg/spliti/plugin/render3d"
	"github.com/hvuhsg/spliti/plugin/render3d/m"
	"github.com/hvuhsg/spliti/plugin/screenshot"
	"github.com/hvuhsg/spliti/scene"
	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
)

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
	// mode. In M1 the editor has no Play mode, so gated systems never run —
	// the world stays still for editing.
	GameSystems func(a *app.App)
	// Registry lists the component types the inspector can edit. Nil gets
	// the engine built-ins only.
	Registry *registry.Registry
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

	// source model + writeback
	src       *srcmodel.SceneFile
	srcErr    error                // last parse error (scene shown read-only)
	dirty     map[string]time.Time // instance name → write deadline
	statusMsg string
	statusAt  time.Time

	// per-widget Euler cache for quaternion fields (activation-scoped so a
	// drag never feeds back through the lossy quat→Euler conversion).
	eulerCache map[string][3]float32
}

const writebackDebounce = 500 * time.Millisecond

// Build wires the editor systems around the game's own.
func (p Plugin) Build(a *app.App) {
	reg := p.Registry
	if reg == nil {
		reg = registry.New()
		registry.Builtin(reg)
	}
	st := &state{
		cfg:        p,
		reg:        reg,
		op:         gizmo.Translate,
		cam:        defaultRig(),
		dirty:      make(map[string]time.Time),
		eulerCache: make(map[string][3]float32),
	}
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
		if p.LoadAssets != nil {
			p.LoadAssets(c)
		}
		if p.SetupScene != nil {
			p.SetupScene(c)
		}
		st.loadSceneSource()
	})

	render3d.AddPreRender(a, func(c *app.Ctx) {
		st.cam.apply(c)
		drawGrid(c)
	})
	a.AddSystems(schedule.Update, editorUI)
	a.AddSystems(schedule.Last, flushWriteback)

	installSmokeHooks(a, st)
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
	if !imgui.CurrentIO().WantCaptureKeyboard() {
		switch {
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
	drawShell(c, st)
	drawHierarchy(c, st)
	drawInspector(c, st)
	drawViewport(c, st)
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

// markTransformDirty schedules a debounced source write for the instance.
func (st *state) markTransformDirty(instance string) {
	if instance == "" || st.src == nil {
		return
	}
	st.dirty[instance] = time.Now().Add(writebackDebounce)
}

// vecOf converts an imgui vec for the gizmo frame.
func vec2(v imgui.Vec2) m.Vec2 { return m.Vec2{X: v.X, Y: v.Y} }
