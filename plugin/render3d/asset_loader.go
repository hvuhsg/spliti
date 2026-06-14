package render3d

import (
	"io/fs"
	"log"
	"sync"
	"sync/atomic"

	"github.com/hvuhsg/spliti/app"
	"github.com/hvuhsg/spliti/schedule"
)

// LoadStatus is the lifecycle of an asynchronous asset load.
type LoadStatus uint32

const (
	LoadPending LoadStatus = iota // decode in flight, not yet uploaded
	LoadReady                     // uploaded into the registry under its ref
	LoadFailed                    // decode or upload failed; see AssetHandle.Err
)

// AssetHandle is a concurrency-safe, queryable status for one async load. The
// loader hands one back from each Load*Async call. Game code polls Status/Done
// (typically once a frame) and reacts when the asset becomes Ready — meanwhile
// entities that reference the not-yet-loaded ref simply render nothing (mesh) or
// the default material, so spawning before the load completes is safe.
type AssetHandle struct {
	Ref   string
	state atomic.Uint32
	errp  atomic.Pointer[errBox]
}

type errBox struct{ err error }

// Status returns the current load status.
func (h *AssetHandle) Status() LoadStatus { return LoadStatus(h.state.Load()) }

// Done reports whether the load has finished (Ready or Failed).
func (h *AssetHandle) Done() bool {
	s := h.Status()
	return s == LoadReady || s == LoadFailed
}

// Err returns the failure cause when Status is LoadFailed, else nil.
func (h *AssetHandle) Err() error {
	if b := h.errp.Load(); b != nil {
		return b.err
	}
	return nil
}

func (h *AssetHandle) set(s LoadStatus) { h.state.Store(uint32(s)) }

func (h *AssetHandle) fail(err error) {
	h.errp.Store(&errBox{err: err})
	h.state.Store(uint32(LoadFailed))
}

// assetKind discriminates what a request decodes.
type assetKind uint8

const (
	kindMesh  assetKind = iota // a single OBJ mesh
	kindModel                  // a glTF/GLB model (meshes + materials + node tree)
)

// loadRequest captures everything needed to (re-)decode an asset, so hot-reload
// can replay it under the same ref. fsys nil means read from disk.
type loadRequest struct {
	kind assetKind
	file string
	fsys fs.FS
}

// completed crosses the goroutine boundary: a finished CPU decode plus its handle.
// Exactly one of mesh/model is set on success; err is set on failure.
type completed struct {
	handle *AssetHandle
	ref    string
	kind   assetKind
	mesh   *Mesh
	model  *ModelData
	err    error
}

// fileWatcher is the hot-reload hook. The native build implements it with
// fsnotify; the js build is a no-op. nil on the loader means hot-reload is off.
type fileWatcher interface {
	track(absOrFile, ref string) // begin watching the file backing ref
	pending() []string           // refs whose files changed since the last call
	close()
}

// AssetLoader decodes meshes and models on background goroutines and uploads the
// results to the GPU registries on the main thread, a few frames later, via the
// drainLoads system. It is the async counterpart to the synchronous LoadOBJ /
// LoadGLTF helpers. Retrieve it with app.GetResource[AssetLoader].
type AssetLoader struct {
	meshes    *MeshRegistry
	materials *MaterialRegistry

	done     chan completed
	inflight atomic.Int32

	mu       sync.Mutex
	handles  map[string]*AssetHandle
	models   map[string]*Model
	requests map[string]loadRequest // ref -> how to re-decode (for hot-reload)

	watcher fileWatcher
	logf    func(format string, args ...any)
}

// newAssetLoader builds the loader. When hotReload is true it also starts a file
// watcher (native only; a no-op on js). A watcher that fails to start is logged
// and skipped — loading still works, just without live reload.
func newAssetLoader(meshes *MeshRegistry, materials *MaterialRegistry, hotReload bool) *AssetLoader {
	l := &AssetLoader{
		meshes:    meshes,
		materials: materials,
		done:      make(chan completed, 64),
		handles:   make(map[string]*AssetHandle),
		models:    make(map[string]*Model),
		requests:  make(map[string]loadRequest),
		logf:      log.Printf,
	}
	if hotReload {
		w, err := newAssetWatcher(l.logf)
		if err != nil {
			l.logf("render3d: asset hot-reload unavailable: %v", err)
		} else {
			l.watcher = w
		}
	}
	return l
}

// LoadMeshAsync decodes an OBJ file on a goroutine and uploads it under ref once
// ready. Returns immediately with a Pending handle.
func (l *AssetLoader) LoadMeshAsync(ref, file string) *AssetHandle {
	return l.request(ref, loadRequest{kind: kindMesh, file: file})
}

// LoadMeshFSAsync is LoadMeshAsync reading from an fs.FS (e.g. an embed.FS).
// FS-backed assets are immutable, so they are never hot-reloaded.
func (l *AssetLoader) LoadMeshFSAsync(ref string, fsys fs.FS, file string) *AssetHandle {
	return l.request(ref, loadRequest{kind: kindMesh, file: file, fsys: fsys})
}

// LoadModelAsync decodes a glTF/GLB file on a goroutine and uploads its meshes
// and materials under ref-prefixed refs once ready. Once the handle is Ready,
// retrieve the node tree with Model(ref) and instantiate it with SpawnModel.
func (l *AssetLoader) LoadModelAsync(ref, file string) *AssetHandle {
	return l.request(ref, loadRequest{kind: kindModel, file: file})
}

// LoadModelFSAsync is LoadModelAsync reading from an fs.FS.
func (l *AssetLoader) LoadModelFSAsync(ref string, fsys fs.FS, file string) *AssetHandle {
	return l.request(ref, loadRequest{kind: kindModel, file: file, fsys: fsys})
}

// Model returns the uploaded model tree for ref once its load is Ready, else nil.
func (l *AssetLoader) Model(ref string) *Model {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.models[ref]
}

// Handle returns the handle for ref, or nil if no load was requested for it.
func (l *AssetLoader) Handle(ref string) *AssetHandle {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.handles[ref]
}

// request records the (re-)load, resets its handle to Pending, registers it with
// the watcher (disk-backed only), and kicks off the background decode.
func (l *AssetLoader) request(ref string, req loadRequest) *AssetHandle {
	l.mu.Lock()
	h := l.handles[ref]
	if h == nil {
		h = &AssetHandle{Ref: ref}
		l.handles[ref] = h
	}
	h.set(LoadPending)
	l.requests[ref] = req
	l.mu.Unlock()

	if l.watcher != nil && req.fsys == nil {
		l.watcher.track(req.file, ref)
	}
	l.startDecode(ref, h, req)
	return h
}

// startDecode runs the CPU decode on a goroutine and posts the result to done.
func (l *AssetLoader) startDecode(ref string, h *AssetHandle, req loadRequest) {
	l.inflight.Add(1)
	go func() {
		c := completed{handle: h, ref: ref, kind: req.kind}
		switch req.kind {
		case kindMesh:
			if req.fsys != nil {
				c.mesh, c.err = LoadOBJFS(req.fsys, req.file)
			} else {
				c.mesh, c.err = LoadOBJ(req.file)
			}
		case kindModel:
			if req.fsys != nil {
				c.model, c.err = DecodeGLTFFS(ref, req.fsys, req.file)
			} else {
				c.model, c.err = DecodeGLTF(ref, req.file)
			}
		}
		l.done <- c
	}()
}

// apply runs on the main thread: it performs the GPU upload (or records the
// failure) and advances the handle. A failed load never panics — the error lands
// on the handle and the referencing entities keep degrading gracefully.
func (l *AssetLoader) apply(c completed) {
	defer l.inflight.Add(-1)
	if c.err != nil {
		c.handle.fail(c.err)
		l.logf("render3d: async load %q failed: %v", c.ref, c.err)
		return
	}
	switch c.kind {
	case kindMesh:
		// l.meshes is nil only in headless tests, where the state machine is
		// exercised without a GPU; skip the upload but still advance the handle.
		if l.meshes != nil {
			if err := l.meshes.Load(c.ref, c.mesh); err != nil {
				c.handle.fail(err)
				l.logf("render3d: upload mesh %q failed: %v", c.ref, err)
				return
			}
		}
	case kindModel:
		if l.meshes != nil && l.materials != nil {
			model, err := c.model.upload(l.meshes, l.materials)
			if err != nil {
				c.handle.fail(err)
				l.logf("render3d: upload model %q failed: %v", c.ref, err)
				return
			}
			l.mu.Lock()
			l.models[c.ref] = model
			l.mu.Unlock()
		}
	}
	c.handle.set(LoadReady)
}

// close stops the watcher. Safe to call once on exit.
func (l *AssetLoader) close() {
	if l.watcher != nil {
		l.watcher.close()
		l.watcher = nil
	}
}

// drainWatch re-issues a decode for every asset whose file changed on disk. No-op
// when hot-reload is off.
func (l *AssetLoader) drainWatch() {
	if l.watcher == nil {
		return
	}
	for _, ref := range l.watcher.pending() {
		l.mu.Lock()
		req, ok := l.requests[ref]
		h := l.handles[ref]
		l.mu.Unlock()
		if !ok || h == nil {
			continue
		}
		h.set(LoadPending)
		l.logf("render3d: asset %q changed on disk, reloading", ref)
		l.startDecode(ref, h, req)
	}
}

// drainLoads is the schedule.First system that performs every ready upload on the
// main thread and re-issues any hot-reload requests. It never blocks: it drains
// whatever decodes have completed and returns.
func drainLoads(c *app.Ctx) {
	l := app.GetResource[AssetLoader](c)
	if l == nil {
		return
	}
	l.drainWatch()
	for {
		select {
		case done := <-l.done:
			l.apply(done)
		default:
			return
		}
	}
}

// loaderLabel orders the async-upload system within schedule.First.
const loaderLabel = "__render3d_loader"

// installLoaderSystem registers drainLoads. Split out so installSystems stays
// focused on the render chain.
func installLoaderSystem(a *app.App) {
	a.AddSystems(schedule.First, app.System(drainLoads).Label(loaderLabel))
}
