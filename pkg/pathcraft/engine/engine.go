package engine

import (
	"fmt"
	"time"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/osm"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
)

type Engine struct {
	graph     *graph.Graph
	gtfsIndex *gtfs.StopTimeIndex
}

func New() *Engine {
	return &Engine{}
}

type RouteRequest struct {
	From               int64
	To                 int64
	Profile            mobility.Profile
	IncludeCoordinates bool
}

type Coordinate struct {
	Lat float64
	Lon float64
}

type RouteResult struct {
	Nodes       []int64
	Coordinates []Coordinate
	Distance    float64       // Total distance in meters
	Duration    time.Duration // Estimated duration
}

type GraphStats struct {
	Nodes int
	Edges int
}

func (e *Engine) LoadOSM(path string) error {
	data, err := osm.ParseFile(path)
	if err != nil {
		return fmt.Errorf("parsing OSM file: %w", err)
	}

	e.graph = osm.BuildGraph(data, nil)
	return nil
}

func (e *Engine) SaveGraph(path string) error {
	if e.graph == nil {
		return fmt.Errorf("graph not loaded")
	}
	return e.graph.Save(path)
}

func (e *Engine) LoadGraph(path string) error {
	g, err := graph.LoadGraph(path)
	if err != nil {
		return err
	}
	e.graph = g
	return nil
}

func (e *Engine) LoadGTFS(stopTimesPath, tripsPath string) error {
	stopTimes, err := gtfs.ParseStopTimesFile(stopTimesPath)
	if err != nil {
		return fmt.Errorf("parsing stop_times: %w", err)
	}

	tripRoutes, err := gtfs.ParseTripsFile(tripsPath)
	if err != nil {
		return fmt.Errorf("parsing trips: %w", err)
	}

	e.gtfsIndex = gtfs.BuildIndex(stopTimes, tripRoutes)
	return nil
}

type TransitRouteRequest struct {
	FromStop      string
	ToStop        string
	DepartureTime string // HH:MM:SS
}

func (e *Engine) TransitRoute(req TransitRouteRequest) (*raptor.Result, error) {
	if e.gtfsIndex == nil {
		return nil, fmt.Errorf("GTFS not loaded")
	}

	depTime, err := pcTime.ParseTime(req.DepartureTime)
	if err != nil {
		return nil, fmt.Errorf("invalid departure time: %w", err)
	}

	router := raptor.NewRouter(e.gtfsIndex, nil)
	res := router.Search(gtfs.StopID(req.FromStop), depTime)

	return res, nil
}

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

	speed := req.Profile.Speed()
	if speed <= 0 {
		speed = mobility.DefaultWalkingSpeedMPS
	}

	heuristic := geo.HaversineHeuristic(speed)
	path, err := astar.AStar(e.graph, sourceID, targetID, heuristic)
	if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}

	nodes := make([]int64, len(path.Nodes))
	var coords []Coordinate
	if req.IncludeCoordinates {
		coords = make([]Coordinate, len(path.Nodes))
	}

	for i, n := range path.Nodes {
		nodes[i] = int64(n)
		if req.IncludeCoordinates {
			node := e.graph.Nodes[n]
			coords[i] = Coordinate{
				Lat: node.Lat,
				Lon: node.Lon,
			}
		}
	}

	durationSeconds := path.TotalCost / speed
	duration := time.Duration(durationSeconds * float64(time.Second))

	return &RouteResult{
		Nodes:       nodes,
		Coordinates: coords,
		Distance:    path.TotalCost,
		Duration:    duration,
	}, nil
}

func (e *Engine) Stats() GraphStats {
	if e.graph == nil {
		return GraphStats{}
	}

	edgeCount := 0
	for _, edges := range e.graph.Edges {
		edgeCount += len(edges)
	}

	return GraphStats{
		Nodes: len(e.graph.Nodes),
		Edges: edgeCount,
	}
}

func (e *Engine) NearestNode(lat, lon float64) (int64, float64, error) {
	if e.graph == nil {
		return 0, 0, fmt.Errorf("graph not loaded")
	}

	id, dist := e.graph.NearestNode(lat, lon, geo.HaversineDistance)
	return int64(id), dist, nil
}

// GetGraph returns the underlying graph.
// Note: This exposes internal implementation details and should be used with caution.
// It is primarily intended for the HTTP server adapter.
func (e *Engine) GetGraph() *graph.Graph {
	return e.graph
}
