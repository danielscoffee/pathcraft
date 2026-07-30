package worldgraph

type Node struct {
	ID    int64
	Lon   float64
	Lat   float64
	Owner TileID
}

type EdgeID struct {
	WayID int64
	From  int64
	To    int64
}

type Edge struct {
	ID              EdgeID
	DistanceMeters  float64
	Highway         string
	Name            string
	RestrictWalking bool
	RestrictDriving bool
	Owner           TileID
	Sources         []string
}

type Chunk struct {
	Tile  TileID
	Nodes []Node
	Edges []Edge
}
