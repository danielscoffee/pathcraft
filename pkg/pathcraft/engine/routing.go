package engine

import (
	"fmt"
	"time"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/geojson"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
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

	if req.Profile == nil {
		return nil, fmt.Errorf("routing profile is required")
	}

	return e.routeBetweenNodes(sourceID, targetID, req.Profile, req.IncludeCoordinates)
}

func (e *Engine) RouteByCoordinates(req CoordinateRouteRequest) (*CoordinateRouteResult, error) {
	if e.graph == nil {
		return nil, fmt.Errorf("graph not loaded")
	}
	if req.Profile == nil {
		return nil, fmt.Errorf("routing profile is required")
	}

	fromNodeID, fromSnapDist, err := e.NearestNode(req.FromLat, req.FromLon)
	if err != nil {
		return nil, err
	}

	toNodeID, toSnapDist, err := e.NearestNode(req.ToLat, req.ToLon)
	if err != nil {
		return nil, err
	}

	route, err := e.routeBetweenNodes(graph.NodeID(fromNodeID), graph.NodeID(toNodeID), req.Profile, req.IncludeCoordinates)
	if err != nil {
		return nil, err
	}

	if req.IncludeCoordinates && req.IncludeInputInShape {
		route.Coordinates = append([]Coordinate{{Lat: req.FromLat, Lon: req.FromLon}}, route.Coordinates...)
		route.Coordinates = append(route.Coordinates, Coordinate{Lat: req.ToLat, Lon: req.ToLon})
	}

	return &CoordinateRouteResult{
		RouteResult:       *route,
		FromNodeID:        fromNodeID,
		ToNodeID:          toNodeID,
		FromSnapDistanceM: fromSnapDist,
		ToSnapDistanceM:   toSnapDist,
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

	return GraphStats{Nodes: len(e.graph.Nodes), Edges: edgeCount}
}

func (e *Engine) NearestNode(lat, lon float64) (int64, float64, error) {
	if e.graph == nil {
		return 0, 0, fmt.Errorf("graph not loaded")
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
	speed := profile.Speed()
	if speed <= 0 {
		speed = mobility.DefaultWalkingSpeedMPS
	}

	heuristic := geo.HaversineHeuristic(speed)
	path, err := astar.AStarWithProfile(e.graph, sourceID, targetID, heuristic, profile)
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
	duration := time.Duration(durationSeconds * float64(time.Second))

	return &RouteResult{Nodes: nodes, Coordinates: coords, Distance: path.TotalCost, Duration: duration}, nil
}
