package prop

import "github.com/hvuhsg/spliti/plugin/render3d/m"

// Grid is a horizontal sampling lattice at a fixed height Y, spanning [MinX,MaxX]
// × [MinZ,MaxZ] with NX×NZ cells. It defines where the coverage field is
// evaluated (e.g. a receiver-height plane over the street).
type Grid struct {
	MinX, MinZ float32
	MaxX, MaxZ float32
	NX, NZ     int
	Y          float32
}

// Len returns the number of cells (NX*NZ).
func (g Grid) Len() int { return g.NX * g.NZ }

// Cell returns the world-space center of cell (ix,iz).
func (g Grid) Cell(ix, iz int) m.Vec3 {
	fx := (float32(ix) + 0.5) / float32(g.NX)
	fz := (float32(iz) + 0.5) / float32(g.NZ)
	return m.Vec3{
		X: g.MinX + (g.MaxX-g.MinX)*fx,
		Y: g.Y,
		Z: g.MinZ + (g.MaxZ-g.MinZ)*fz,
	}
}

// Index maps (ix,iz) to the flat array index used by Coverage.
func (g Grid) Index(ix, iz int) int { return iz*g.NX + ix }

// Coverage evaluates the received power (watts) at every grid cell from tx,
// including direct and reflected paths. The result is a flat NX*NZ field indexed
// by Grid.Index, ready to drive a heatmap. This is the many-query hot path — pass
// a BVH. Cells coincident with the transmitter are skipped (left at 0).
func Coverage(tx Tx, grid Grid, faces []Face, bvh *BVH, cfg Config) []float64 {
	field := make([]float64, grid.Len())
	for iz := 0; iz < grid.NZ; iz++ {
		for ix := 0; ix < grid.NX; ix++ {
			p := grid.Cell(ix, iz)
			if p.Sub(tx.Pos).LengthSq() < 1e-4 {
				continue
			}
			paths := Propagate(tx, p, faces, bvh, cfg)
			pw, _ := Received(paths)
			field[grid.Index(ix, iz)] = pw
		}
	}
	return field
}
