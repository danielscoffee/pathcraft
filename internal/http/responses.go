package http

import (
	"time"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func toJourneyResponse(res *engine.MultimodalRouteResult) journeyResponse {
	out := journeyResponse{
		Mode:                   res.Mode,
		DepartureTime:          res.DepartureTime,
		ArrivalTime:            res.ArrivalTime,
		TotalDurationSeconds:   int64(res.TotalDuration / time.Second),
		TransitDurationSeconds: int64(res.TransitDuration / time.Second),
		WalkingDistanceM:       res.WalkingDistanceM,
		OriginStopID:           res.OriginStopID,
		DestinationStopID:      res.DestinationStopID,
		TransitPath:            make([]journeyLegResponse, 0, len(res.TransitPath)),
		Legs:                   make([]journeyLegResponse, 0, len(res.Legs)),
	}

	for _, leg := range res.TransitPath {
		out.TransitPath = append(out.TransitPath, toJourneyLegResponse(leg))
	}
	for _, leg := range res.Legs {
		out.Legs = append(out.Legs, toJourneyLegResponse(leg))
	}

	return out
}

func toJourneyLegResponse(leg engine.JourneyLeg) journeyLegResponse {
	return journeyLegResponse{
		Mode:            leg.Mode,
		FromName:        leg.FromName,
		ToName:          leg.ToName,
		FromStopID:      leg.FromStopID,
		ToStopID:        leg.ToStopID,
		TripID:          leg.TripID,
		RouteID:         leg.RouteID,
		RouteName:       leg.RouteName,
		RouteLongName:   leg.RouteLongName,
		DistanceM:       leg.DistanceM,
		DurationSeconds: int64(leg.Duration / time.Second),
		Nodes:           leg.Nodes,
		Coordinates:     leg.Coordinates,
	}
}
