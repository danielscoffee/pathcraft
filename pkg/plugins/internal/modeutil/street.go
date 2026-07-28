package modeutil

import (
	"context"
	"fmt"
	"math"

	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

type StreetHost interface {
	RouteByCoordinates(engine.CoordinateRouteRequest) (*engine.CoordinateRouteResult, error)
}

func StreetRoute(ctx context.Context, host any, req core.ModeRequest, manifest core.ModeManifest, profile mobility.Profile) (core.ModeResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ModeResult{}, err
	}
	router, ok := host.(StreetHost)
	if !ok {
		return core.ModeResult{}, fmt.Errorf("mode %s requires a street routing host", manifest.ID)
	}
	fromLon, fromLat, err := GeographicPosition("from", req.From)
	if err != nil {
		return core.ModeResult{}, err
	}
	toLon, toLat, err := GeographicPosition("to", req.To)
	if err != nil {
		return core.ModeResult{}, err
	}

	result, err := router.RouteByCoordinates(engine.CoordinateRouteRequest{
		FromLat:            fromLat,
		FromLon:            fromLon,
		ToLat:              toLat,
		ToLon:              toLon,
		Profile:            profile,
		IncludeCoordinates: true,
	})
	if err != nil {
		return core.ModeResult{}, err
	}
	positions := make([]core.Position, len(result.Coordinates))
	for i, coordinate := range result.Coordinates {
		positions[i] = core.Position{coordinate.Lon, coordinate.Lat}
	}

	return core.ModeResult{
		Mode:            manifest.ID,
		DurationSeconds: int64(math.Ceil(result.Duration.Seconds())),
		DistanceMeters:  result.Distance,
		Segments: []core.RouteSegment{{
			Kind:            manifest.ID,
			Label:           manifest.Label,
			Color:           manifest.Color,
			Positions:       positions,
			DistanceMeters:  result.Distance,
			DurationSeconds: int64(math.Ceil(result.Duration.Seconds())),
		}},
		Meta: map[string]any{
			"from_node_id": result.FromNodeID,
			"to_node_id":   result.ToNodeID,
		},
	}, nil
}

func GeographicPosition(name string, position core.Position) (lon, lat float64, err error) {
	if len(position) < 2 {
		return 0, 0, fmt.Errorf("%s position requires longitude and latitude", name)
	}
	lon, lat = position[0], position[1]
	if math.IsNaN(lon) || math.IsInf(lon, 0) || math.IsNaN(lat) || math.IsInf(lat, 0) {
		return 0, 0, fmt.Errorf("%s position must be finite", name)
	}
	if lon < -180 || lon > 180 || lat < -90 || lat > 90 {
		return 0, 0, fmt.Errorf("%s position is outside geographic bounds", name)
	}
	return lon, lat, nil
}
