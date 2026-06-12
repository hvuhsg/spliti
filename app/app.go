package app

import (
	"fmt"
	"reflect"

	"github.com/hvuhsg/spliti/schedule"
	"github.com/mlange-42/arche/ecs"
	"github.com/mlange-42/arche/listener"
)

// App is the spliti application builder and runner.
//
// Lifecycle: New() → AddPlugins / AddSystems → Run(). Run resolves system
// ordering, executes the startup stages once, then loops the main stages until
// Stop or MaxFrames is reached.
type App struct {
	world  ecs.World
	ctx    *Ctx
	stages map[schedule.Stage]*stageSystems

	resources      map[reflect.Type]any
	eventsDrainers []eventsDrainer

	stateMachines []*stateMachine
	stateByType   map[reflect.Type]*stateMachine

	// Change tracking: a single arche listener (Dispatch) multiplexes per-type
	// Callback sub-listeners installed by TrackChanges[T]. Buffers are drained
	// once per frame, after Last.
	dispatch        listener.Dispatch
	changeBuffers   map[reflect.Type]*changeBuffer
	changeListeners []ecs.Listener

	running   bool
	frame     int
	maxFrames int

	// hooks installed by plugins. Run between StateTransition and Update.
	preUpdateHook  func()
	postUpdateHook func()

	// systemInterceptor, when set, rewrites every system registered through
	// AddSystems before it is stored. See SetSystemInterceptor.
	systemInterceptor func(schedule.Stage, *SystemConfig) *SystemConfig

	onExitHooks []func()
}

// New constructs an empty App with a fresh arche World.
func New() *App {
	a := &App{
		world:         ecs.NewWorld(),
		stages:        make(map[schedule.Stage]*stageSystems),
		resources:     make(map[reflect.Type]any),
		stateByType:   make(map[reflect.Type]*stateMachine),
		changeBuffers: make(map[reflect.Type]*changeBuffer),
		dispatch:      listener.NewDispatch(),
	}
	a.ctx = &Ctx{world: &a.world, app: a, commands: newCommands()}
	a.world.SetListener(&a.dispatch)
	return a
}

// AddSystems registers one or more systems on the given stage. Each item may
// be a SystemFunc, a *SystemConfig, or a []*SystemConfig (e.g. from Chain).
func (a *App) AddSystems(stage schedule.Stage, items ...any) *App {
	st, ok := a.stages[stage]
	if !ok {
		st = &stageSystems{}
		a.stages[stage] = st
	}
	add := func(cfg *SystemConfig) {
		if a.systemInterceptor != nil {
			cfg = a.systemInterceptor(stage, cfg)
			if cfg == nil {
				return
			}
		}
		st.add(systemEntry{
			fn:     cfg.Fn,
			label:  cfg.label,
			runIf:  cfg.runIf,
			before: cfg.before,
			after:  cfg.after,
		})
	}
	for _, item := range items {
		switch v := item.(type) {
		case SystemFunc:
			add(&SystemConfig{Fn: v})
		case *SystemConfig:
			add(v)
		case []*SystemConfig:
			for _, c := range v {
				add(c)
			}
		default:
			panic(fmt.Sprintf("AddSystems: unsupported argument type %T", v))
		}
	}
	return a
}

// SetSystemInterceptor installs a hook that sees every system registered via
// AddSystems (from plugins and user code alike) before it is stored. The hook
// may return the config unchanged, return a modified config (e.g. with an
// extra RunIf gate), or return nil to drop the system. The editor uses it to
// gate game systems behind Play mode. Install before AddPlugins/AddSystems;
// systems registered earlier are unaffected.
func (a *App) SetSystemInterceptor(fn func(schedule.Stage, *SystemConfig) *SystemConfig) {
	a.systemInterceptor = fn
}

// AddPlugins invokes each plugin's Build method against this App.
func (a *App) AddPlugins(plugins ...Plugin) *App {
	for _, p := range plugins {
		p.Build(a)
	}
	return a
}

// SetMaxFrames bounds the main loop to n frames; 0 means unbounded.
func (a *App) SetMaxFrames(n int) *App {
	a.maxFrames = n
	return a
}

// Stop signals the run loop to exit at the end of the current frame.
func (a *App) Stop() { a.running = false }

// Frame returns the number of completed main-loop iterations.
func (a *App) Frame() int { return a.frame }

// Ctx exposes the App's context. Plugins use this during Build for one-shot
// setup that needs World access.
func (a *App) Ctx() *Ctx { return a.ctx }

// AddOnExit registers a cleanup callback. Hooks run in reverse-registration
// order after the main loop exits — including via panic — so plugins can
// release terminals, files, sockets, etc.
func (a *App) AddOnExit(fn func()) { a.onExitHooks = append(a.onExitHooks, fn) }

// Run resolves ordering, executes startup stages once, then loops main stages.
// Cleanup hooks run on normal exit and on panic.
func (a *App) Run() {
	defer func() {
		for i := len(a.onExitHooks) - 1; i >= 0; i-- {
			a.onExitHooks[i]()
		}
	}()
	a.resolveAll()

	for _, st := range schedule.StartupStages {
		a.runStage(st)
	}
	a.fireInitialOnEnter()

	a.running = true
	a.driveLoop()
}

// step runs exactly one frame: every main stage in order, then the per-frame
// event/change drains and the time plugin's pacing hooks. It is the body of the
// run loop, factored out so the platform-specific driver (a blocking for-loop on
// native, a requestAnimationFrame callback on js/wasm) can invoke it. It returns
// false once the app should stop (Stop was called or MaxFrames was reached).
func (a *App) step() bool {
	a.runStage(schedule.First)
	a.runStage(schedule.PreUpdate)

	a.applyStateTransitions()
	a.runStage(schedule.StateTransition)

	if a.preUpdateHook != nil {
		a.preUpdateHook()
	}

	a.runStage(schedule.Update)
	a.runStage(schedule.PostUpdate)
	a.runStage(schedule.Last)

	a.drainAllEvents()
	a.drainAllChanges()

	if a.postUpdateHook != nil {
		a.postUpdateHook()
	}

	a.frame++
	if a.maxFrames > 0 && a.frame >= a.maxFrames {
		a.running = false
	}
	return a.running
}

func (a *App) resolveAll() {
	for st, ss := range a.stages {
		if err := ss.resolve(); err != nil {
			panic(fmt.Errorf("stage %s: %w", st, err))
		}
	}
}

func (a *App) runStage(st schedule.Stage) {
	ss, ok := a.stages[st]
	if !ok {
		return
	}
	for _, idx := range ss.order {
		entry := ss.entries[idx]
		if !entry.shouldRun(a.ctx) {
			continue
		}
		entry.fn(a.ctx)
	}
	a.ctx.commands.apply(&a.world)
}

// SetPreUpdateHook installs a callback run after StateTransition and before
// Update. Used internally by the time plugin to drive FixedUpdate.
func (a *App) SetPreUpdateHook(fn func()) { a.preUpdateHook = fn }

// SetPostUpdateHook installs a callback run after Last. Used internally by
// the time plugin for frame pacing.
func (a *App) SetPostUpdateHook(fn func()) { a.postUpdateHook = fn }

// RunStage runs the systems registered for st once. Exposed for plugins (e.g.
// the time plugin's fixed-loop hook). Most user code should not call this.
func (a *App) RunStage(st schedule.Stage) { a.runStage(st) }
