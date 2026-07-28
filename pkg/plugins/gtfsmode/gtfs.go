package gtfsmode

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
	"github.com/danielscoffee/pathcraft/pkg/plugins/internal/modeutil"
)

type multimodalHost interface {
	MultimodalRoute(engine.MultimodalRouteRequest) (*engine.MultimodalRouteResult, error)
}

type Plugin struct{}

func (Plugin) Name() string { return "gtfs" }

func (Plugin) Manifest() core.ModeManifest {
	return core.ModeManifest{
		ID:         "gtfs",
		Label:      "Bus / GTFS",
		Icon:       "bus",
		Color:      "#1d4ed8",
		CRS:        "EPSG:4326",
		Dimensions: []int{2},
		Axes:       []string{"longitude", "latitude"},
		Options: []core.ModeOption{
			{Name: "departure_time", Label: "Depart", Kind: "time", Default: "05:00:00"},
			{Name: "max_stop_count", Label: "Stop candidates", Kind: "number", Default: "8"},
		},
	}
}

func (Plugin) Route(ctx context.Context, host any, req core.ModeRequest) (core.ModeResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ModeResult{}, err
	}
	router, ok := host.(multimodalHost)
	if !ok {
		return core.ModeResult{}, fmt.Errorf("mode gtfs requires a multimodal routing host")
	}
	fromLon, fromLat, err := modeutil.GeographicPosition("from", req.From)
	if err != nil {
		return core.ModeResult{}, err
	}
	toLon, toLat, err := modeutil.GeographicPosition("to", req.To)
	if err != nil {
		return core.ModeResult{}, err
	}
	departure := req.Options["departure_time"]
	if departure == "" {
		departure = "05:00:00"
	}
	maxStopCount := 0
	if value := req.Options["max_stop_count"]; value != "" {
		maxStopCount, err = strconv.Atoi(value)
		if err != nil || maxStopCount < 0 {
			return core.ModeResult{}, fmt.Errorf("max_stop_count must be a non-negative integer")
		}
	}

	journey, err := router.MultimodalRoute(engine.MultimodalRouteRequest{
		FromLat:        fromLat,
		FromLon:        fromLon,
		ToLat:          toLat,
		ToLon:          toLon,
		DepartureTime:  departure,
		WalkingProfile: mobility.NewWalking(mobility.DefaultWalkingSpeedMPS),
		MaxStopCount:   maxStopCount,
	})
	if err != nil {
		return core.ModeResult{}, err
	}

	segments := make([]core.RouteSegment, 0, len(journey.Legs))
	for _, leg := range journey.Legs {
		positions := make([]core.Position, len(leg.Coordinates))
		for i, coordinate := range leg.Coordinates {
			positions[i] = core.Position{coordinate.Lon, coordinate.Lat}
		}
		color, dashed := segmentStyle(leg.Mode)
		segments = append(segments, core.RouteSegment{
			Kind:            leg.Mode,
			Label:           segmentLabel(leg),
			Color:           color,
			Dashed:          dashed,
			Positions:       positions,
			DistanceMeters:  leg.DistanceM,
			DurationSeconds: int64(math.Ceil(leg.Duration.Seconds())),
			Meta: map[string]any{
				"from":            leg.FromName,
				"to":              leg.ToName,
				"from_stop_id":    leg.FromStopID,
				"to_stop_id":      leg.ToStopID,
				"trip_id":         leg.TripID,
				"route_id":        leg.RouteID,
				"route_name":      leg.RouteName,
				"route_long_name": leg.RouteLongName,
				"departure_time":  leg.DepartureTime,
				"arrival_time":    leg.ArrivalTime,
			},
		})
	}

	return core.ModeResult{
		Mode:            "gtfs",
		DurationSeconds: int64(math.Ceil(journey.TotalDuration.Seconds())),
		DistanceMeters:  journey.WalkingDistanceM,
		Segments:        segments,
		Meta: map[string]any{
			"journey_mode":        journey.Mode,
			"departure_time":      journey.DepartureTime,
			"arrival_time":        journey.ArrivalTime,
			"origin_stop_id":      journey.OriginStopID,
			"destination_stop_id": journey.DestinationStopID,
		},
	}, nil
}

func segmentStyle(kind string) (string, bool) {
	switch kind {
	case "transit":
		return "#1d4ed8", true
	case "transfer":
		return "#b45309", true
	default:
		return "#c2451e", false
	}
}

func segmentLabel(leg engine.JourneyLeg) string {
	if leg.RouteName != "" {
		return leg.RouteName
	}
	if leg.RouteID != "" {
		return leg.RouteID
	}
	if leg.FromName != "" || leg.ToName != "" {
		return leg.FromName + " → " + leg.ToName
	}
	return leg.Mode
}

func init() { plugins.MustRegisterMode(Plugin{}) }
