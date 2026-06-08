// Package astar registers the "astar" Algorithm that wraps the internal
// A* implementation. It requires a core.Graph that exposes the internal
// graph via the osmgraph.Native capability interface.
package astar

import (
	"context"
	"fmt"
	"time"

	"github.com/danielscoffee/pathcraft/internal/geo"
	iastar "github.com/danielscoffee/pathcraft/internal/routing/astar"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/osmgraph"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/registry"
)

const defaultSpeedMPS = 1.4

type Plugin struct{}

func (Plugin) Name() string { return "astar" }

func (Plugin) Route(_ context.Context, g core.Graph, req core.RouteRequest) (core.RouteResult, error) {
	native, ok := g.(osmgraph.Native)
	if !ok {
		return core.RouteResult{}, fmt.Errorf("astar: graph does not support native access (need osmgraph.Native)")
	}
	ig := native.NativeGraph()

	from, ok := osmgraph.ParseID(req.From)
	if !ok {
		return core.RouteResult{}, fmt.Errorf("astar: invalid from node id %q", req.From)
	}
	to, ok := osmgraph.ParseID(req.To)
	if !ok {
		return core.RouteResult{}, fmt.Errorf("astar: invalid to node id %q", req.To)
	}

	speed := defaultSpeedMPS
	if v, ok := req.Options["speed_mps"].(float64); ok && v > 0 {
		speed = v
	}

	start := time.Now()
	path, err := iastar.AStar(ig, from, to, geo.HaversineHeuristic(speed))
	if err != nil {
		return core.RouteResult{}, fmt.Errorf("astar: %w", err)
	}

	pathIDs := make([]core.NodeID, len(path.Nodes))
	for i, n := range path.Nodes {
		pathIDs[i] = osmgraph.FormatID(n)
	}

	return core.RouteResult{
		Path:         pathIDs,
		Cost:         path.TotalCost,
		DurationMS:   time.Since(start).Milliseconds(),
		VisitedNodes: path.NodesCount,
	}, nil
}

func init() { registry.MustRegisterAlgorithm(Plugin{}) }
