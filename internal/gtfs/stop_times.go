// Package gtfs provides parsers and data structures for GTFS (General Transit Feed Specification) data.
// This package focuses on structures optimized for the RAPTOR algorithm.
package gtfs

import (
	"errors"
	"fmt"
	"sort"

	"github.com/danielscoffee/pathcraft/internal/time"
)

type StopID string

type TripID string

type RouteID string

type StopTime struct {
	TripID        TripID
	StopID        StopID
	ArrivalTime   time.Time
	DepartureTime time.Time
	StopSequence  int
}

type RouteStop struct {
	StopID   StopID
	Sequence int
}

type RoutePattern struct {
	RouteID RouteID
	Stops   []RouteStop
}

type TripStopTime struct {
	TripID        TripID
	ArrivalTime   time.Time
	DepartureTime time.Time
}

type StopTimeIndex struct {
	StopRoutes          map[StopID][]RouteID
	RoutePatterns       map[RouteID]*RoutePattern
	RouteStopTrips      map[string][]TripStopTime
	StopPositionInRoute map[string]int
	// RouteTrips[routeID][tripIndex][stopIndex]
	RouteTrips map[RouteID][][]TripStopTime
}

func NewStopTimeIndex() *StopTimeIndex {
	return &StopTimeIndex{
		StopRoutes:          make(map[StopID][]RouteID),
		RoutePatterns:       make(map[RouteID]*RoutePattern),
		RouteStopTrips:      make(map[string][]TripStopTime),
		StopPositionInRoute: make(map[string]int),
		RouteTrips:          make(map[RouteID][][]TripStopTime),
	}
}

func (idx *StopTimeIndex) RoutesAtStop(stopID StopID) []RouteID {
	return idx.StopRoutes[stopID]
}

func (idx *StopTimeIndex) StopsOnRoute(routeID RouteID) []RouteStop {
	pattern := idx.RoutePatterns[routeID]
	if pattern == nil {
		return nil
	}
	return pattern.Stops
}

func (idx *StopTimeIndex) TripsAtRouteStop(routeID RouteID, stopSequence int) []TripStopTime {
	key := fmt.Sprintf("%s:%d", routeID, stopSequence)
	return idx.RouteStopTrips[key]
}

func (idx *StopTimeIndex) GetStopSequence(stopID StopID, routeID RouteID) int {
	key := fmt.Sprintf("%s:%s", stopID, routeID)
	seq, ok := idx.StopPositionInRoute[key]
	if !ok {
		return -1
	}
	return seq
}

func (idx *StopTimeIndex) EarliestTrip(routeID RouteID, stopSequence int, minDepartureTime time.Time) *TripStopTime {
	trips := idx.TripsAtRouteStop(routeID, stopSequence)
	if len(trips) == 0 {
		return nil
	}

	i := sort.Search(len(trips), func(i int) bool {
		return trips[i].DepartureTime >= minDepartureTime
	})

	if i < len(trips) {
		return &trips[i]
	}
	return nil
}

func (idx *StopTimeIndex) EarliestTripIndex(routeID RouteID, stopIndex int, minDepartureTime time.Time) int {
	trips := idx.RouteTrips[routeID]
	if len(trips) == 0 {
		return -1
	}

	i := sort.Search(len(trips), func(i int) bool {
		return trips[i][stopIndex].DepartureTime >= minDepartureTime
	})

	if i < len(trips) {
		return i
	}
	return -1
}

// WARN: dedicate error packages?
var (
	ErrMissingColumn = errors.New("missing required column")
	ErrInvalidData   = errors.New("invalid data")
)

type TripToRoute map[TripID]RouteID

func BuildIndex(stopTimes []StopTime, tripRoutes TripToRoute) *StopTimeIndex {
	idx := NewStopTimeIndex()

	tripStops := make(map[TripID][]StopTime)
	for _, st := range stopTimes {
		tripStops[st.TripID] = append(tripStops[st.TripID], st)
	}

	for tripID := range tripStops {
		sort.Slice(tripStops[tripID], func(i, j int) bool {
			return tripStops[tripID][i].StopSequence < tripStops[tripID][j].StopSequence
		})
	}

	routeStopsSet := make(map[RouteID]map[int]StopID)
	routeStopTripsTemp := make(map[string][]TripStopTime)

	for tripID, stops := range tripStops {
		routeID, ok := tripRoutes[tripID]
		if !ok {
			continue
		}

		if routeStopsSet[routeID] == nil {
			routeStopsSet[routeID] = make(map[int]StopID)
		}

		for _, st := range stops {
			routeStopsSet[routeID][st.StopSequence] = st.StopID

			key := fmt.Sprintf("%s:%d", routeID, st.StopSequence)
			routeStopTripsTemp[key] = append(routeStopTripsTemp[key], TripStopTime{
				TripID:        tripID,
				ArrivalTime:   st.ArrivalTime,
				DepartureTime: st.DepartureTime,
			})

			posKey := fmt.Sprintf("%s:%s", st.StopID, routeID)
			idx.StopPositionInRoute[posKey] = st.StopSequence
		}
	}

	for routeID, seqToStop := range routeStopsSet {
		var sequences []int
		for seq := range seqToStop {
			sequences = append(sequences, seq)
		}
		sort.Ints(sequences)

		pattern := &RoutePattern{
			RouteID: routeID,
			Stops:   make([]RouteStop, len(sequences)),
		}

		for i, seq := range sequences {
			stopID := seqToStop[seq]
			pattern.Stops[i] = RouteStop{
				StopID:   stopID,
				Sequence: seq,
			}

			if !containsRoute(idx.StopRoutes[stopID], routeID) {
				idx.StopRoutes[stopID] = append(idx.StopRoutes[stopID], routeID)
			}
		}

		idx.RoutePatterns[routeID] = pattern
	}

	for key, trips := range routeStopTripsTemp {
		sort.Slice(trips, func(i, j int) bool {
			return trips[i].DepartureTime < trips[j].DepartureTime
		})
		idx.RouteStopTrips[key] = trips
	}

	for routeID, pattern := range idx.RoutePatterns {
		firstStopSeq := pattern.Stops[0].Sequence
		firstStopTrips := idx.TripsAtRouteStop(routeID, firstStopSeq)

		routeTrips := make([][]TripStopTime, 0, len(firstStopTrips))
		for _, firstStopTrip := range firstStopTrips {
			tripID := firstStopTrip.TripID
			tripData := make([]TripStopTime, len(pattern.Stops))

			validTrip := true
			for j, rs := range pattern.Stops {
				allTripsAtStop := idx.TripsAtRouteStop(routeID, rs.Sequence)
				found := false
				for _, t := range allTripsAtStop {
					if t.TripID == tripID {
						tripData[j] = t
						found = true
						break
					}
				}
				if !found {
					validTrip = false
					break
				}
			}

			if validTrip {
				routeTrips = append(routeTrips, tripData)
			}
		}

		sort.Slice(routeTrips, func(i, j int) bool {
			return routeTrips[i][0].DepartureTime < routeTrips[j][0].DepartureTime
		})

		idx.RouteTrips[routeID] = routeTrips
	}

	return idx
}

func containsRoute(routes []RouteID, target RouteID) bool {
	for _, r := range routes {
		if r == target {
			return true
		}
	}
	return false
}
