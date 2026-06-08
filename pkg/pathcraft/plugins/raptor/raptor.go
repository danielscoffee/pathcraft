// Package raptor registers the "raptor" Algorithm that wraps the internal
// RAPTOR transit router. The input core.Graph must implement gtfsgraph.Native.
//
// RouteRequest options:
//   - "departure_time" (string, "HH:MM:SS"): required.
package raptor

import (
	"context"
	"fmt"
	"time"

	"github.com/danielscoffee/pathcraft/internal/gtfs"
	iraptor "github.com/danielscoffee/pathcraft/internal/routing/raptor"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/gtfsgraph"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/registry"
)

type Plugin struct{}

func (Plugin) Name() string { return "raptor" }

func (Plugin) Route(_ context.Context, g core.Graph, req core.RouteRequest) (core.RouteResult, error) {
	native, ok := g.(gtfsgraph.Native)
	if !ok {
		return core.RouteResult{}, fmt.Errorf("raptor: graph does not implement gtfsgraph.Native")
	}

	depStr, _ := req.Options["departure_time"].(string)
	if depStr == "" {
		return core.RouteResult{}, fmt.Errorf("raptor: options.departure_time is required (HH:MM:SS)")
	}
	dep, err := pcTime.ParseTime(depStr)
	if err != nil {
		return core.RouteResult{}, fmt.Errorf("raptor: invalid departure_time: %w", err)
	}

	router := iraptor.NewRouter(native.NativeIndex(), native.NativeTransfers())
	start := time.Now()
	result := router.Search(gtfs.StopID(req.From), dep)

	target := gtfs.StopID(req.To)
	arrival, reached := result.EarliestArrival[target]
	if !reached {
		return core.RouteResult{}, fmt.Errorf("raptor: stop %s not reachable from %s", req.To, req.From)
	}

	steps := result.ReconstructPath(target)
	path := make([]core.NodeID, 0, len(steps)+1)
	if len(steps) > 0 {
		path = append(path, core.NodeID(steps[0].FromStop))
	} else {
		path = append(path, req.From)
	}
	for _, s := range steps {
		path = append(path, core.NodeID(s.ToStop))
	}

	return core.RouteResult{
		Path:         path,
		Cost:         float64(arrival - dep),
		DurationMS:   time.Since(start).Milliseconds(),
		VisitedNodes: len(result.EarliestArrival),
		Meta: map[string]any{
			"departure_time": dep.String(),
			"arrival_time":   arrival.String(),
		},
	}, nil
}

func init() { registry.MustRegisterAlgorithm(Plugin{}) }
