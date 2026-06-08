package core

import "context"

// Graph is the minimum a routing source must expose: given a node, return
// its outgoing edges. Algorithms call Neighbors as they explore.
type Graph interface {
	Neighbors(ctx context.Context, node NodeID) ([]Edge, error)
}

// Coordinated is an optional capability for graphs whose nodes have
// geographic coordinates. Heuristic algorithms (A*) type-assert for this.
type Coordinated interface {
	Coord(node NodeID) (lat, lon float64, ok bool)
}

// Sized is an optional capability for graphs that know their node count.
type Sized interface {
	NodeCount() int
}
