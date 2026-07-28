package http

import (
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func modeResultToJourneyResponse(result core.ModeResult) journeyResponse {
	response := journeyResponse{
		Mode:                 modeMetaString(result.Meta, "journey_mode"),
		DepartureTime:        modeMetaString(result.Meta, "departure_time"),
		ArrivalTime:          modeMetaString(result.Meta, "arrival_time"),
		TotalDurationSeconds: result.DurationSeconds,
		WalkingDistanceM:     result.DistanceMeters,
		OriginStopID:         modeMetaString(result.Meta, "origin_stop_id"),
		DestinationStopID:    modeMetaString(result.Meta, "destination_stop_id"),
		TransitPath:          make([]journeyLegResponse, 0, len(result.Segments)),
		Legs:                 make([]journeyLegResponse, 0, len(result.Segments)),
	}
	if response.Mode == "" {
		response.Mode = result.Mode
	}
	for _, segment := range result.Segments {
		leg := modeSegmentToJourneyLeg(segment)
		response.Legs = append(response.Legs, leg)
		if segment.Kind == "transit" || segment.Kind == "transfer" {
			response.TransitPath = append(response.TransitPath, leg)
			response.TransitDurationSeconds += segment.DurationSeconds
		}
	}
	return response
}

func modeSegmentToJourneyLeg(segment core.RouteSegment) journeyLegResponse {
	coordinates := make([]engine.Coordinate, 0, len(segment.Positions))
	for _, position := range segment.Positions {
		if len(position) >= 2 {
			coordinates = append(coordinates, engine.Coordinate{Lon: position[0], Lat: position[1]})
		}
	}
	return journeyLegResponse{
		Mode:            segment.Kind,
		FromName:        modeMetaString(segment.Meta, "from"),
		ToName:          modeMetaString(segment.Meta, "to"),
		FromStopID:      modeMetaString(segment.Meta, "from_stop_id"),
		ToStopID:        modeMetaString(segment.Meta, "to_stop_id"),
		TripID:          modeMetaString(segment.Meta, "trip_id"),
		RouteID:         modeMetaString(segment.Meta, "route_id"),
		RouteName:       modeMetaString(segment.Meta, "route_name"),
		RouteLongName:   modeMetaString(segment.Meta, "route_long_name"),
		DepartureTime:   modeMetaString(segment.Meta, "departure_time"),
		ArrivalTime:     modeMetaString(segment.Meta, "arrival_time"),
		DistanceM:       segment.DistanceMeters,
		DurationSeconds: segment.DurationSeconds,
		Coordinates:     coordinates,
	}
}

func modeMetaString(meta map[string]any, key string) string {
	value, _ := meta[key].(string)
	return value
}
