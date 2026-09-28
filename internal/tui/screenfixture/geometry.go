package screenfixture

// Geometry is one terminal a baseline is recorded at.
type Geometry struct {
	Name          string
	Width, Height int
}

// Geometries are the three terminals every screen baseline is recorded at: the
// 80x24 floor where every body wraps and most bodies overflow, the 120x40
// default, and a wide terminal where the same content has room to breathe.
var Geometries = []Geometry{
	{Name: "80x24", Width: 80, Height: 24},
	{Name: "120x40", Width: 120, Height: 40},
	{Name: "200x50", Width: 200, Height: 50},
}
