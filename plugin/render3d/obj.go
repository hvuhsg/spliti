package render3d

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/hvuhsg/spliti/plugin/render3d/m"
)

// ParseOBJ decodes a Wavefront .obj from data into a single CPU mesh combining
// all of its faces. It supports the v / vt / vn attribute formats in any
// combination (v, v/vt, v//vn, v/vt/vn), polygon faces (triangulated as a fan),
// and negative (relative) indices. Object/group/smoothing directives, material
// libraries, and line/point elements are ignored — only triangle geometry is
// kept. Faces that omit normals get flat-ish normals from computeNormals;
// tangents are filled by fillTangents (these meshes carry no normal maps, so the
// exact tangent is arbitrary). Like the primitive generators, this produces
// model-space geometry ready for MeshRegistry.Load — no GPU is required.
func ParseOBJ(data []byte) (*Mesh, error) {
	var (
		positions []m.Vec3
		normals   []m.Vec3
		uvs       []m.Vec2
	)
	mesh := &Mesh{}
	// Dedup identical (position, uv, normal) corners so shared face corners reuse
	// one vertex; key components are absolute 0-based indices, -1 when absent.
	seen := make(map[[3]int]uint32)
	sawNormals := false

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // some OBJs have long lines
	for ln := 1; sc.Scan(); ln++ {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "v":
			p, err := objVec3(fields[1:])
			if err != nil {
				return nil, fmt.Errorf("obj line %d: v: %w", ln, err)
			}
			positions = append(positions, p)
		case "vn":
			n, err := objVec3(fields[1:])
			if err != nil {
				return nil, fmt.Errorf("obj line %d: vn: %w", ln, err)
			}
			normals = append(normals, n)
		case "vt":
			if len(fields) < 3 {
				return nil, fmt.Errorf("obj line %d: vt needs 2 components", ln)
			}
			u, err1 := strconv.ParseFloat(fields[1], 32)
			vv, err2 := strconv.ParseFloat(fields[2], 32)
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("obj line %d: vt: bad float", ln)
			}
			// OBJ texture space has its origin at the bottom-left (V up), while
			// render3d (matching its glTF loader) samples with V down: flip it.
			uvs = append(uvs, m.Vec2{X: float32(u), Y: 1 - float32(vv)})
		case "f":
			corners := fields[1:]
			if len(corners) < 3 {
				return nil, fmt.Errorf("obj line %d: face has %d corners", ln, len(corners))
			}
			ring := make([]uint32, len(corners))
			for i, c := range corners {
				pi, ti, ni, err := objCorner(c, len(positions), len(uvs), len(normals))
				if err != nil {
					return nil, fmt.Errorf("obj line %d: %w", ln, err)
				}
				if ni >= 0 {
					sawNormals = true
				}
				key := [3]int{pi, ti, ni}
				vi, ok := seen[key]
				if !ok {
					v := vert(positions[pi], m.Vec3{Y: 1}, 0, 0, m.Vec4{X: 1, W: 1})
					if ti >= 0 {
						v.U, v.V = uvs[ti].X, uvs[ti].Y
					}
					if ni >= 0 {
						n := normals[ni]
						v.NX, v.NY, v.NZ = n.X, n.Y, n.Z
					}
					vi = uint32(len(mesh.Vertices))
					mesh.Vertices = append(mesh.Vertices, v)
					seen[key] = vi
				}
				ring[i] = vi
			}
			// Fan-triangulate the (assumed convex, planar) polygon.
			for i := 1; i+1 < len(ring); i++ {
				mesh.Indices = append(mesh.Indices, ring[0], ring[i], ring[i+1])
			}
		}
		// All other directives (o, g, s, usemtl, mtllib, l, p, ...) are ignored.
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("obj: read: %w", err)
	}
	if len(mesh.Vertices) == 0 || len(mesh.Indices) == 0 {
		return nil, fmt.Errorf("obj: no triangle geometry")
	}
	if !sawNormals {
		computeNormals(mesh)
	}
	fillTangents(mesh) // repairs any degenerate normals and sets unit tangents
	return mesh, nil
}

// LoadOBJ reads a .obj file from disk and parses it into a mesh (see ParseOBJ).
// It is pure CPU work — feed the result to MeshRegistry.Load to upload it.
func LoadOBJ(file string) (*Mesh, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("render3d: open obj %q: %w", file, err)
	}
	return ParseOBJ(data)
}

// LoadOBJFS is LoadOBJ reading from an fs.FS (e.g. an embed.FS), so models can
// be embedded in the binary.
func LoadOBJFS(fsys fs.FS, file string) (*Mesh, error) {
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return nil, fmt.Errorf("render3d: open obj %q: %w", file, err)
	}
	return ParseOBJ(data)
}

func objVec3(f []string) (m.Vec3, error) {
	if len(f) < 3 {
		return m.Vec3{}, fmt.Errorf("need 3 components, got %d", len(f))
	}
	x, err1 := strconv.ParseFloat(f[0], 32)
	y, err2 := strconv.ParseFloat(f[1], 32)
	z, err3 := strconv.ParseFloat(f[2], 32)
	if err1 != nil || err2 != nil || err3 != nil {
		return m.Vec3{}, fmt.Errorf("bad float")
	}
	return m.Vec3{X: float32(x), Y: float32(y), Z: float32(z)}, nil
}

// objCorner parses one face corner token ("p", "p/t", "p//n", "p/t/n") into
// absolute 0-based position/uv/normal indices (-1 when the attribute is absent).
func objCorner(tok string, nPos, nUV, nNorm int) (p, t, n int, err error) {
	parts := strings.Split(tok, "/")
	if p, err = objIndex(parts[0], nPos); err != nil {
		return 0, 0, 0, fmt.Errorf("face corner %q: %w", tok, err)
	}
	t, n = -1, -1
	if len(parts) >= 2 && parts[1] != "" {
		if t, err = objIndex(parts[1], nUV); err != nil {
			return 0, 0, 0, fmt.Errorf("face corner %q: %w", tok, err)
		}
	}
	if len(parts) >= 3 && parts[2] != "" {
		if n, err = objIndex(parts[2], nNorm); err != nil {
			return 0, 0, 0, fmt.Errorf("face corner %q: %w", tok, err)
		}
	}
	return p, t, n, nil
}

// fillTangents sets each vertex tangent to a unit vector perpendicular to its
// (already computed) normal, so untextured PBR materials never see a degenerate
// tangent. The choice is arbitrary — these meshes carry no normal maps. It also
// repairs degenerate (zero-length) normals by substituting +Y, keeping the
// shader from normalizing a zero vector.
func fillTangents(mesh *Mesh) {
	for i := range mesh.Vertices {
		n := m.Vec3{X: mesh.Vertices[i].NX, Y: mesh.Vertices[i].NY, Z: mesh.Vertices[i].NZ}
		if n.LengthSq() < 1e-6 {
			n = m.Vec3{Y: 1}
			mesh.Vertices[i].NX, mesh.Vertices[i].NY, mesh.Vertices[i].NZ = 0, 1, 0
		}
		ref := m.Vec3{Y: 1}
		if abs32(n.Dot(ref)) > 0.99 {
			ref = m.Vec3{X: 1}
		}
		t := ref.Cross(n).Normalize()
		mesh.Vertices[i].TX, mesh.Vertices[i].TY, mesh.Vertices[i].TZ, mesh.Vertices[i].TW = t.X, t.Y, t.Z, 1
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// objIndex converts a 1-based OBJ index (negative = relative to the end of the
// list parsed so far) into a 0-based index, bounds-checked against count.
func objIndex(s string, count int) (int, error) {
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("bad index %q", s)
	}
	switch {
	case i > 0:
		i--
	case i < 0:
		i += count
	default:
		return 0, fmt.Errorf("index 0 is invalid")
	}
	if i < 0 || i >= count {
		return 0, fmt.Errorf("index %s out of range [1,%d]", s, count)
	}
	return i, nil
}
