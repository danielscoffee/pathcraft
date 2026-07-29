package engine

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/geojson"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
	"github.com/danielscoffee/pathcraft/internal/routing/street"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

func (e *Engine) Route(req RouteRequest) (*RouteResult, error) {
	if e.graph == nil {
		return nil, fmt.Errorf("graph not loaded")
	}

	sourceID := graph.NodeID(req.From)
	targetID := graph.NodeID(req.To)

	if !e.graph.HasNode(sourceID) {
		return nil, fmt.Errorf("source node %d not found", req.From)
	}
	if !e.graph.HasNode(targetID) {
		return nil, fmt.Errorf("target node %d not found", req.To)
	}

	return e.routeBetweenNodes(sourceID, targetID, req.Profile, req.IncludeCoordinates)
}

func (e *Engine) RouteByCoordinates(req CoordinateRouteRequest) (*CoordinateRouteResult, error) {
	return e.RouteByCoordinatesContext(context.Background(), req)
}

func (e *Engine) RouteByCoordinatesContext(ctx context.Context, req CoordinateRouteRequest) (*CoordinateRouteResult, error) {
	result, err := street.Route(ctx, e.graph, street.Request{
		FromLat:            req.FromLat,
		FromLon:            req.FromLon,
		ToLat:              req.ToLat,
		ToLon:              req.ToLon,
		Profile:            e.routeProfile(req.Profile),
		IncludeCoordinates: req.IncludeCoordinates,
	})
	if err != nil {
		return nil, err
	}
	var coordinates []Coordinate
	if len(result.Coordinates) > 0 {
		coordinates = make([]Coordinate, len(result.Coordinates))
		for index, coordinate := range result.Coordinates {
			coordinates[index] = Coordinate{Lat: coordinate.Lat, Lon: coordinate.Lon}
		}
	}
	if req.IncludeCoordinates && req.IncludeInputInShape {
		coordinates = append([]Coordinate{{Lat: req.FromLat, Lon: req.FromLon}}, coordinates...)
		coordinates = append(coordinates, Coordinate{Lat: req.ToLat, Lon: req.ToLon})
	}
	return &CoordinateRouteResult{
		RouteResult: RouteResult{
			Nodes:       result.Nodes,
			Coordinates: coordinates,
			Distance:    result.Distance,
			Duration:    result.Duration,
		},
		FromNodeID:        result.FromNodeID,
		ToNodeID:          result.ToNodeID,
		FromSnapDistanceM: result.FromSnapDistanceM,
		ToSnapDistanceM:   result.ToSnapDistanceM,
	}, nil
}

func (e *Engine) RouteGeoJSON(req RouteRequest) ([]byte, error) {
	res, err := e.Route(req)
	if err != nil {
		return nil, err
	}

	ids := make([]graph.NodeID, len(res.Nodes))
	for i, nodeID := range res.Nodes {
		ids[i] = graph.NodeID(nodeID)
	}

	return geojson.PathToGeoJSON(e.graph, ids), nil
}

func (e *Engine) RouteGeoJSONByCoordinates(req CoordinateRouteRequest) ([]byte, error) {
	res, err := e.RouteByCoordinates(req)
	if err != nil {
		return nil, err
	}

	ids := make([]graph.NodeID, len(res.Nodes))
	for i, nodeID := range res.Nodes {
		ids[i] = graph.NodeID(nodeID)
	}

	return geojson.PathToGeoJSON(e.graph, ids), nil
}

func (e *Engine) Stats() GraphStats {
	if e.graph == nil {
		return GraphStats{}
	}

	edgeCount := 0
	for _, edges := range e.graph.Edges {
		edgeCount += len(edges)
	}
	stats := GraphStats{Nodes: len(e.graph.Nodes), Edges: edgeCount}
	if index := e.graph.Contraction; index != nil {
		retained := 0
		for _, keep := range index.Retained {
			if keep {
				retained++
			}
		}
		stats.ContractedNodes = len(e.graph.Nodes) - retained
		stats.ContractionChains = len(index.Chains)
	}
	return stats
}

func (e *Engine) NearestNode(lat, lon float64) (int64, float64, error) {
	if e.graph == nil {
		return 0, 0, fmt.Errorf("graph not loaded")
	}
	if math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lon) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, fmt.Errorf("coordinates must be finite and within geographic bounds")
	}

	id, dist := e.graph.NearestNode(lat, lon, geo.HaversineDistance)
	return int64(id), dist, nil
}

func (e *Engine) GetGraph() *graph.Graph {
	return e.graph
}

func (e *Engine) SetNearestNodeIndex(index plugins.NearestNodeIndex) error {
	if e.graph == nil {
		return fmt.Errorf("graph not loaded")
	}

	e.graph.SetNearestNodeIndex(index)
	return nil
}

func (e *Engine) routeBetweenNodes(sourceID, targetID graph.NodeID, profile mobility.Profile, includeCoordinates bool) (*RouteResult, error) {
	profile = e.routeProfile(profile)
	speed := profile.Speed()
	if speed <= 0 {
		speed = mobility.DefaultWalkingSpeedMPS
	}
	if math.IsNaN(speed) || math.IsInf(speed, 0) {
		return nil, fmt.Errorf("routing profile speed must be finite")
	}

	path, err := astar.AStarWithProfile(e.graph, sourceID, targetID, geo.HaversineHeuristic(1), profile)
	if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}

	nodes := make([]int64, len(path.Nodes))
	var coords []Coordinate
	if includeCoordinates {
		coords = make([]Coordinate, len(path.Nodes))
	}

	for i, n := range path.Nodes {
		nodes[i] = int64(n)
		if includeCoordinates {
			node := e.graph.Nodes[n]
			coords[i] = Coordinate{Lat: node.Lat, Lon: node.Lon}
		}
	}

	durationSeconds := path.TotalCost / speed
	if math.IsNaN(durationSeconds) || math.IsInf(durationSeconds, 0) || durationSeconds > float64(math.MaxInt64)/float64(time.Second) {
		return nil, fmt.Errorf("route duration is out of range")
	}
	duration := time.Duration(durationSeconds * float64(time.Second))

	return &RouteResult{Nodes: nodes, Coordinates: coords, Distance: path.TotalDistance, Duration: duration}, nil
}
