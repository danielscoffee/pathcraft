package engine

import (
	"fmt"
	"sort"

	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
)

func (e *Engine) TransitRoute(req TransitRouteRequest) (*raptor.Result, error) {
	if e.gtfsIndex == nil {
		return nil, fmt.Errorf("GTFS not loaded")
	}
	if req.FromStop == "" || req.ToStop == "" {
		return nil, fmt.Errorf("from and to stops are required")
	}

	depTime, err := pcTime.ParseTime(req.DepartureTime)
	if err != nil {
		return nil, fmt.Errorf("invalid departure time: %w", err)
	}

	router := raptor.NewRouter(e.gtfsIndex, e.gtfsTransfers)
	res := router.Search(gtfs.StopID(req.FromStop), depTime)

	targetStop := gtfs.StopID(req.ToStop)
	if _, ok := res.EarliestArrival[targetStop]; !ok {
		return nil, fmt.Errorf("stop %s is not reachable from %s", req.ToStop, req.FromStop)
	}

	return res, nil
}

func (e *Engine) GTFSStops() []GTFSStop {
	if len(e.gtfsStops) == 0 {
		return nil
	}

	stops := make([]GTFSStop, 0, len(e.gtfsStops))
	for _, stop := range e.gtfsStops {
		stops = append(stops, GTFSStop{
			ID:   string(stop.ID),
			Name: stop.Name,
			Lat:  stop.Lat,
			Lon:  stop.Lon,
		})
	}

	sort.Slice(stops, func(i, j int) bool {
		if stops[i].Name == stops[j].Name {
			return stops[i].ID < stops[j].ID
		}
		return stops[i].Name < stops[j].Name
	})

	return stops
}

func (e *Engine) GTFSTripIDs() []string {
	if e.gtfsIndex == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var tripIDs []string
	for _, routeTrips := range e.gtfsIndex.RouteTrips {
		for _, trip := range routeTrips {
			if len(trip) == 0 {
				continue
			}
			tripID := string(trip[0].TripID)
			if _, ok := seen[tripID]; ok {
				continue
			}
			seen[tripID] = struct{}{}
			tripIDs = append(tripIDs, tripID)
		}
	}

	sort.Strings(tripIDs)
	return tripIDs
}

func (e *Engine) GTFSTripStopTimes(tripID string) ([]GTFSTripStopTime, error) {
	if e.gtfsIndex == nil {
		return nil, fmt.Errorf("GTFS not loaded")
	}
	if len(e.gtfsStops) == 0 {
		return nil, fmt.Errorf("GTFS stops with coordinates are required")
	}
	if tripID == "" {
		return nil, fmt.Errorf("trip_id is required")
	}

	targetTripID := gtfs.TripID(tripID)
	for routeID, routeTrips := range e.gtfsIndex.RouteTrips {
		pattern := e.gtfsIndex.RoutePatterns[routeID]
		if pattern == nil {
			continue
		}

		for _, trip := range routeTrips {
			if len(trip) == 0 || trip[0].TripID != targetTripID {
				continue
			}

			stopTimes := make([]GTFSTripStopTime, 0, len(trip))
			for i, stopTime := range trip {
				if i >= len(pattern.Stops) {
					break
				}
				stopID := pattern.Stops[i].StopID
				stop := e.gtfsStops[stopID]
				stopTimes = append(stopTimes, GTFSTripStopTime{
					TripID:        string(stopTime.TripID),
					RouteID:       string(routeID),
					StopID:        string(stopID),
					StopName:      stop.Name,
					ArrivalTime:   stopTime.ArrivalTime.String(),
					DepartureTime: stopTime.DepartureTime.String(),
					StopSequence:  pattern.Stops[i].Sequence,
					Lat:           stop.Lat,
					Lon:           stop.Lon,
				})
			}

			return stopTimes, nil
		}
	}

	return nil, fmt.Errorf("trip %s not found", tripID)
}
