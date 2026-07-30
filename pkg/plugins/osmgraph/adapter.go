// Package osmgraph adapts the internal *graph.Graph (used by OSM and the
// A* implementation) to the public core.Graph interface.
//
// It exposes a Native capability interface so plugins that need direct
// access to the internal graph (e.g. the astar plugin's heuristic search)
// can recover it via type assertion without reimplementing search.
package osmgraph

import (
	"context"
	"strconv"

	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
)

// Native is the escape hatch: any core.Graph that wraps an internal
// *graph.Graph implements this. Algorithm plugins type-assert for it to
// avoid going through the generic core.Graph.Neighbors path.
type Native interface {
	NativeGraph() *graph.Graph
}

// Wrapper implements core.Graph, core.Coordinated, core.Sized, and Native.
type Wrapper struct {
	G *graph.Graph
}

// Wrap returns a core.Graph backed by g.
func Wrap(g *graph.Graph) *Wrapper { return &Wrapper{G: g} }

func (w *Wrapper) NativeGraph() *graph.Graph { return w.G }

func (w *Wrapper) NodeCount() int { return len(w.G.Nodes) }

func (w *Wrapper) Coord(id core.NodeID) (float64, float64, bool) {
	n, ok := parseID(id)
	if !ok {
		return 0, 0, false
	}
	node, exists := w.G.Nodes[n]
	if !exists {
		return 0, 0, false
	}
	return node.Lat, node.Lon, true
}

func (w *Wrapper) Neighbors(_ context.Context, id core.NodeID) ([]core.Edge, error) {
	n, ok := parseID(id)
	if !ok {
		return nil, nil
	}
	edges := w.G.Neighbors(n)
	out := make([]core.Edge, 0, len(edges))
	for _, e := range edges {
		out = append(out, core.Edge{
			From: id,
			To:   FormatID(e.To),
			Cost: e.DistanceM,
		})
	}
	return out, nil
}

// FormatID converts an internal graph.NodeID into a public core.NodeID.
func FormatID(id graph.NodeID) core.NodeID {
	return core.NodeID(strconv.FormatInt(int64(id), 10))
}

// ParseID extracts an internal graph.NodeID from a public core.NodeID.
func ParseID(id core.NodeID) (graph.NodeID, bool) { return parseID(id) }

func parseID(id core.NodeID) (graph.NodeID, bool) {
	v, err := strconv.ParseInt(string(id), 10, 64)
	if err != nil {
		return 0, false
	}
	return graph.NodeID(v), true
}
