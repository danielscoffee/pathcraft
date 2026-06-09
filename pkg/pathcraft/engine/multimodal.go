package engine

import (
	"fmt"
	"sort"
	"time"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
)

func (e *Engine) MultimodalRoute(req MultimodalRouteRequest) (*MultimodalRouteResult, error) {
	if e.graph == nil {
		return nil, fmt.Errorf("graph not loaded")
	}
	if e.gtfsIndex == nil {
		return nil, fmt.Errorf("GTFS not loaded")
	}
	if len(e.gtfsStops) == 0 {
		return nil, fmt.Errorf("GTFS stops with coordinates are required for multimodal routing")
	}
	if req.WalkingProfile == nil {
		req.WalkingProfile = mobility.NewWalking(mobility.DefaultWalkingSpeedMPS)
	}
	if req.MaxStopCount <= 0 {
		req.MaxStopCount = defaultStopCandidates
	}

	departure, err := pcTime.ParseTime(req.DepartureTime)
	if err != nil {
		return nil, fmt.Errorf("invalid departure time: %w", err)
	}

	directWalk, err := e.RouteByCoordinates(CoordinateRouteRequest{
		FromLat:            req.FromLat,
		FromLon:            req.FromLon,
		ToLat:              req.ToLat,
		ToLon:              req.ToLon,
		Profile:            req.WalkingProfile,
		IncludeCoordinates: true,
	})
	if err != nil {
		return nil, err
	}

	best := &MultimodalRouteResult{
		Mode:             "walk",
		DepartureTime:    departure.String(),
		ArrivalTime:      addDuration(departure, directWalk.Duration).String(),
		TotalDuration:    directWalk.Duration,
		WalkingDistanceM: directWalk.Distance,
		Legs: []JourneyLeg{{
			Mode:        "walk",
			FromName:    "origin",
			ToName:      "destination",
			DistanceM:   directWalk.Distance,
			Duration:    directWalk.Duration,
			Nodes:       directWalk.Nodes,
			Coordinates: directWalk.Coordinates,
		}},
	}

	originNodeID := graph.NodeID(directWalk.FromNodeID)
	destinationNodeID := graph.NodeID(directWalk.ToNodeID)
	originCandidates := e.nearestStops(req.FromLat, req.FromLon, req.MaxStopCount)
	destinationCandidates := e.nearestStops(req.ToLat, req.ToLon, req.MaxStopCount)
	router := raptor.NewRouter(e.gtfsIndex, e.gtfsTransfers)

	for _, fromStop := range originCandidates {
		walkToTransit, err := e.routeBetweenNodes(originNodeID, graph.NodeID(fromStop.nodeID), req.WalkingProfile, true)
		if err != nil {
			continue
		}

		searchDeparture := addDuration(departure, walkToTransit.Duration)
		transitResult := router.Search(fromStop.stop.ID, searchDeparture)

		for _, toStop := range destinationCandidates {
			if toStop.stop.ID == fromStop.stop.ID {
				continue
			}

			transitArrival, ok := transitResult.EarliestArrival[toStop.stop.ID]
			if !ok {
				continue
			}

			walkFromTransit, err := e.routeBetweenNodes(graph.NodeID(toStop.nodeID), destinationNodeID, req.WalkingProfile, true)
			if err != nil {
				continue
			}

			totalDuration := durationBetween(departure, transitArrival) + walkFromTransit.Duration
			if totalDuration >= best.TotalDuration {
				continue
			}

			legs := []JourneyLeg{{
				Mode:        "walk",
				FromName:    "origin",
				ToName:      fromStop.stop.Name,
				FromStopID:  string(fromStop.stop.ID),
				DistanceM:   walkToTransit.Distance,
				Duration:    walkToTransit.Duration,
				Nodes:       walkToTransit.Nodes,
				Coordinates: walkToTransit.Coordinates,
			}}

			transitSteps := transitResult.ReconstructPath(toStop.stop.ID)
			if len(transitSteps) == 0 {
				continue
			}
			transitDuration := durationBetween(searchDeparture, transitArrival)
			for _, step := range transitSteps {
				leg := JourneyLeg{
					Mode:       "transit",
					FromName:   e.stopLabel(step.FromStop),
					ToName:     e.stopLabel(step.ToStop),
					FromStopID: string(step.FromStop),
					ToStopID:   string(step.ToStop),
					TripID:     string(step.TripID),
				}
				leg.Coordinates = e.transitLegCoordinates(step.TripID, step.FromStop, step.ToStop)
				if routeID, ok := e.gtfsTripRoutes[step.TripID]; ok {
					leg.RouteID = string(routeID)
					leg.RouteName = e.routeShortLabel(routeID)
					leg.RouteLongName = e.routeLongLabel(routeID)
				}
				if step.IsTransfer {
					leg.Mode = "transfer"
					leg.Duration = time.Duration(step.Duration) * time.Second
					leg.Coordinates = e.stopPairCoordinates(step.FromStop, step.ToStop)
					leg.DistanceM = coordinateLineDistance(leg.Coordinates)
				}
				legs = append(legs, leg)
			}

			legs = append(legs, JourneyLeg{
				Mode:        "walk",
				FromName:    toStop.stop.Name,
				ToName:      "destination",
				ToStopID:    string(toStop.stop.ID),
				DistanceM:   walkFromTransit.Distance,
				Duration:    walkFromTransit.Duration,
				Nodes:       walkFromTransit.Nodes,
				Coordinates: walkFromTransit.Coordinates,
			})

			best = &MultimodalRouteResult{
				Mode:              "multimodal",
				DepartureTime:     departure.String(),
				ArrivalTime:       addDuration(transitArrival, walkFromTransit.Duration).String(),
				TotalDuration:     totalDuration,
				TransitDuration:   transitDuration,
				WalkingDistanceM:  walkToTransit.Distance + walkFromTransit.Distance,
				TransitPath:       legs[1 : len(legs)-1],
				Legs:              legs,
				OriginStopID:      string(fromStop.stop.ID),
				DestinationStopID: string(toStop.stop.ID),
			}
		}
	}

	return best, nil
}

func (e *Engine) nearestStops(lat, lon float64, limit int) []stopCandidate {
	if limit <= 0 {
		return nil
	}

	candidates := make([]stopCandidate, 0, len(e.gtfsStops))
	for _, stop := range e.gtfsStops {
		nodeID, snapDist, err := e.NearestNode(stop.Lat, stop.Lon)
		if err != nil {
			continue
		}
		candidates = append(candidates, stopCandidate{
			stop:      stop,
			distanceM: geo.HaversineDistance(lat, lon, stop.Lat, stop.Lon),
			nodeID:    nodeID,
			snapDistM: snapDist,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].distanceM < candidates[j].distanceM
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	return candidates
}
