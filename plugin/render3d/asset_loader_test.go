package render3d

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qmuntal/gltf"
)

// recv reads one completed result off the loader's done channel, failing the
// test if nothing arrives promptly (the decode goroutine should be quick).
func recv(t *testing.T, l *AssetLoader) completed {
	t.Helper()
	select {
	case c := <-l.done:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async decode")
		return completed{}
	}
}

func TestDecodeModelNodeTree(t *testing.T) {
	doc := buildTestDoc()
	metallic := 0.5
	doc.Materials = []*gltf.Material{{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorFactor: &[4]float64{1, 0, 0, 1},
			MetallicFactor:  &metallic,
		},
	}}
	doc.Meshes[0].Primitives[0].Material = gltf.Index(0)

	md, err := decodeModel("hero", doc, nil)
	if err != nil {
		t.Fatalf("decodeModel: %v", err)
	}
	if len(md.Materials) != 1 {
		t.Fatalf("got %d materials, want 1", len(md.Materials))
	}
	if len(md.Meshes) != 1 {
		t.Fatalf("got %d meshes, want 1", len(md.Meshes))
	}
	dm := md.Meshes[0]
	if dm.materialIdx != 0 || dm.meshIdx != 0 || dm.primIdx != 0 {
		t.Errorf("decodedMesh = {mat %d, mesh %d, prim %d}, want {0,0,0}", dm.materialIdx, dm.meshIdx, dm.primIdx)
	}
	if dm.mesh == nil || len(dm.mesh.Vertices) != 3 {
		t.Errorf("decoded mesh has wrong vertex count")
	}
	if len(md.Nodes) != 1 || len(md.Nodes[0].PrimIdx) != 1 || md.Nodes[0].PrimIdx[0] != 0 {
		t.Errorf("node PrimIdx = %+v, want [0]", md.Nodes)
	}
	if len(md.Roots) != 1 || md.Roots[0] != 0 {
		t.Errorf("roots = %v, want [0]", md.Roots)
	}
}

func TestModelDataUploadNilRegistry(t *testing.T) {
	md := &ModelData{Name: "x"}
	if _, err := md.upload(nil, nil); err == nil {
		t.Fatal("upload with nil registries should error")
	}
}

const triangleOBJ = "v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 3\n"

func writeOBJ(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "tri.obj")
	if err := os.WriteFile(p, []byte(triangleOBJ), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestAsyncMeshDecode drives a real off-thread OBJ decode and asserts the result
// matches the synchronous ParseOBJ (same geometry, no GPU involved).
func TestAsyncMeshDecode(t *testing.T) {
	path := writeOBJ(t)
	want, err := ParseOBJ([]byte(triangleOBJ))
	if err != nil {
		t.Fatal(err)
	}

	l := newAssetLoader(nil, nil, false) // headless: no registries, no watcher
	h := l.LoadMeshAsync("tri", path)
	if h.Status() != LoadPending {
		t.Fatalf("status = %v immediately after request, want Pending", h.Status())
	}

	c := recv(t, l)
	if c.err != nil {
		t.Fatalf("decode error: %v", c.err)
	}
	if c.kind != kindMesh || c.mesh == nil {
		t.Fatalf("completed = %+v, want a mesh", c)
	}
	if len(c.mesh.Vertices) != len(want.Vertices) || len(c.mesh.Indices) != len(want.Indices) {
		t.Errorf("decoded %d verts/%d idx, ParseOBJ gave %d/%d",
			len(c.mesh.Vertices), len(c.mesh.Indices), len(want.Vertices), len(want.Indices))
	}

	// apply on a headless loader skips the GPU upload but still advances the handle.
	l.apply(c)
	if h.Status() != LoadReady {
		t.Errorf("status after apply = %v, want Ready", h.Status())
	}
	if got := l.inflight.Load(); got != 0 {
		t.Errorf("inflight = %d after apply, want 0", got)
	}
}

// TestAsyncLoadFailure confirms a missing file surfaces as a Failed handle with a
// non-nil error and never panics — the load-failure-recovery contract.
func TestAsyncLoadFailure(t *testing.T) {
	l := newAssetLoader(nil, nil, false)
	h := l.LoadMeshAsync("missing", filepath.Join(t.TempDir(), "does-not-exist.obj"))

	c := recv(t, l)
	if c.err == nil {
		t.Fatal("expected a decode error for a missing file")
	}
	l.apply(c)
	if h.Status() != LoadFailed {
		t.Fatalf("status = %v, want Failed", h.Status())
	}
	if h.Err() == nil {
		t.Error("Err() is nil on a failed load")
	}
	if got := l.inflight.Load(); got != 0 {
		t.Errorf("inflight = %d, want 0", got)
	}
	if l.Handle("missing") != h {
		t.Error("Handle did not return the same handle")
	}
}
