// Package gtfs provides parsers and data structures for GTFS (General Transit Feed Specification) data.
// This package focuses on structures optimized for the RAPTOR algorithm.
package gtfs

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	. "github.com/qoppatech/exp-pathcraft/internal/domain/time"
)

type StopID string

type TripID string

type RouteID string

type StopTime struct {
	TripID        TripID
	StopID        StopID
	ArrivalTime   Time
	DepartureTime Time
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
	ArrivalTime   Time
	DepartureTime Time
}

type StopTimeIndex struct {
	StopRoutes map[StopID][]RouteID

	RoutePatterns map[RouteID]*RoutePattern

	RouteStopTrips map[string][]TripStopTime

	StopPositionInRoute map[string]int
}

func NewStopTimeIndex() *StopTimeIndex {
	return &StopTimeIndex{
		StopRoutes:          make(map[StopID][]RouteID),
		RoutePatterns:       make(map[RouteID]*RoutePattern),
		RouteStopTrips:      make(map[string][]TripStopTime),
		StopPositionInRoute: make(map[string]int),
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

func (idx *StopTimeIndex) EarliestTrip(routeID RouteID, stopSequence int, minDepartureTime Time) *TripStopTime {
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

// WARN: dedicate error packages?
var (
	ErrMissingColumn = errors.New("missing required column")
	ErrInvalidData   = errors.New("invalid data")
)

/// TODO: IF NEEDED SEPARATE PARSING LOGIC TO ANOTHER FILE AND SEPARATE OTHERS FIELDS TO THEIR OWN FILES

func ParseStopTimes(r io.Reader) ([]StopTime, error) {
	csvReader := csv.NewReader(r)

	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}

	requiredCols := []string{"trip_id", "stop_id", "arrival_time", "departure_time", "stop_sequence"}
	for _, col := range requiredCols {
		if _, ok := colIndex[col]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingColumn, col)
		}
	}

	tripIdx := colIndex["trip_id"]
	stopIdx := colIndex["stop_id"]
	arrivalIdx := colIndex["arrival_time"]
	departureIdx := colIndex["departure_time"]
	seqIdx := colIndex["stop_sequence"]

	var stopTimes []StopTime

	lineNum := 1
	for {
		lineNum++
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		arrivalTime, err := ParseTime(record[arrivalIdx])
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid arrival_time: %w", lineNum, err)
		}

		departureTime, err := ParseTime(record[departureIdx])
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid departure_time: %w", lineNum, err)
		}

		stopSequence, err := strconv.Atoi(strings.TrimSpace(record[seqIdx]))
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid stop_sequence: %w", lineNum, err)
		}

		stopTimes = append(stopTimes, StopTime{
			TripID:        TripID(strings.TrimSpace(record[tripIdx])),
			StopID:        StopID(strings.TrimSpace(record[stopIdx])),
			ArrivalTime:   arrivalTime,
			DepartureTime: departureTime,
			StopSequence:  stopSequence,
		})
	}

	return stopTimes, nil
}

func ParseStopTimesFile(path string) ([]StopTime, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseStopTimes(f)
}

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
