package sim

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

// Index maps (ix,iz) to the flat array index used by a coverage field.
func (g Grid) Index(ix, iz int) int { return iz*g.NX + ix }
