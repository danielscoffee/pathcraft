package street

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
)

type Request struct {
	FromLat            float64
	FromLon            float64
	ToLat              float64
	ToLon              float64
	Profile            mobility.Profile
	IncludeCoordinates bool
}

type Coordinate struct {
	Lat float64
	Lon float64
}

type Result struct {
	Nodes             []int64
	Coordinates       []Coordinate
	Distance          float64
	Duration          time.Duration
	FromNodeID        int64
	ToNodeID          int64
	FromSnapDistanceM float64
	ToSnapDistanceM   float64
}

func Route(ctx context.Context, g *graph.Graph, request Request) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g == nil {
		return nil, fmt.Errorf("graph not loaded")
	}
	if err := validatePosition(request.FromLat, request.FromLon); err != nil {
		return nil, fmt.Errorf("from coordinates: %w", err)
	}
	if err := validatePosition(request.ToLat, request.ToLon); err != nil {
		return nil, fmt.Errorf("to coordinates: %w", err)
	}

	fromNodeID, fromSnapDistance := g.NearestNode(request.FromLat, request.FromLon, geo.HaversineDistance)
	toNodeID, toSnapDistance := g.NearestNode(request.ToLat, request.ToLon, geo.HaversineDistance)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	profile := request.Profile
	if profile == nil {
		profile = mobility.NewWalking(mobility.DefaultWalkingSpeedMPS)
	}
	speed := profile.Speed()
	if speed <= 0 {
		speed = mobility.DefaultWalkingSpeedMPS
	}
	if math.IsNaN(speed) || math.IsInf(speed, 0) {
		return nil, fmt.Errorf("routing profile speed must be finite")
	}

	path, err := astar.AStarWithProfileContext(ctx, g, fromNodeID, toNodeID, geo.HaversineHeuristic(1), profile)
	if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}
	nodes := make([]int64, len(path.Nodes))
	var coordinates []Coordinate
	if request.IncludeCoordinates {
		coordinates = make([]Coordinate, len(path.Nodes))
	}
	for index, id := range path.Nodes {
		nodes[index] = int64(id)
		if request.IncludeCoordinates {
			node := g.Nodes[id]
			coordinates[index] = Coordinate{Lat: node.Lat, Lon: node.Lon}
		}
	}

	durationSeconds := path.TotalCost / speed
	if math.IsNaN(durationSeconds) || math.IsInf(durationSeconds, 0) || durationSeconds > float64(math.MaxInt64)/float64(time.Second) {
		return nil, fmt.Errorf("route duration is out of range")
	}
	return &Result{
		Nodes:             nodes,
		Coordinates:       coordinates,
		Distance:          path.TotalDistance,
		Duration:          time.Duration(durationSeconds * float64(time.Second)),
		FromNodeID:        int64(fromNodeID),
		ToNodeID:          int64(toNodeID),
		FromSnapDistanceM: fromSnapDistance,
		ToSnapDistanceM:   toSnapDistance,
	}, nil
}

func validatePosition(lat, lon float64) error {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lon) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return fmt.Errorf("coordinates must be finite and within geographic bounds")
	}
	return nil
}
