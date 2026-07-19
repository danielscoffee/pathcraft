// Package gtfs provides parsers and data structures for GTFS (General Transit Feed Specification) data.
// This package focuses on structures optimized for the RAPTOR algorithm.
package gtfs

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/danielscoffee/pathcraft/internal/time"
)

type StopID string

type TripID string

type RouteID string

type ShapeID string

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
	StopPositionInRoute map[string]int
	// RouteTrips[routeID][tripIndex][stopIndex]
	RouteTrips map[RouteID][][]TripStopTime
}

func NewStopTimeIndex() *StopTimeIndex {
	return &StopTimeIndex{
		StopRoutes:          make(map[StopID][]RouteID),
		RoutePatterns:       make(map[RouteID]*RoutePattern),
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

func (idx *StopTimeIndex) GetStopSequence(stopID StopID, routeID RouteID) int {
	key := fmt.Sprintf("%s:%s", stopID, routeID)
	seq, ok := idx.StopPositionInRoute[key]
	if !ok {
		return -1
	}
	return seq
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
var ErrMissingColumn = errors.New("missing required column")

type Route struct {
	ID        RouteID
	ShortName string
	LongName  string
}

type TripInfo struct {
	TripID  TripID
	RouteID RouteID
	ShapeID ShapeID
}

type ShapePoint struct {
	ShapeID  ShapeID
	Lat      float64
	Lon      float64
	Sequence int
}

type TripToRoute map[TripID]RouteID

type routePatternGroup struct {
	stopIDs []StopID
	trips   map[TripID][]StopTime
}

func BuildIndex(stopTimes []StopTime, tripRoutes TripToRoute) *StopTimeIndex {
	tripStops := make(map[TripID][]StopTime)
	for _, st := range stopTimes {
		tripStops[st.TripID] = append(tripStops[st.TripID], st)
	}
	return buildIndexFromTripStops(tripStops, tripRoutes)
}

// buildIndexFromTripStops is the shared core behind BuildIndex and
// IndexBuilder.Build: rows already grouped by trip, in any order.
func buildIndexFromTripStops(tripStops map[TripID][]StopTime, tripRoutes TripToRoute) *StopTimeIndex {
	idx := NewStopTimeIndex()

	for tripID := range tripStops {
		sort.Slice(tripStops[tripID], func(i, j int) bool {
			return tripStops[tripID][i].StopSequence < tripStops[tripID][j].StopSequence
		})
	}

	groupsByRoute := make(map[RouteID]map[string]*routePatternGroup)
	for tripID, stops := range tripStops {
		routeID, ok := tripRoutes[tripID]
		if !ok || len(stops) == 0 {
			continue
		}

		key := orderedStopKey(stops)
		if groupsByRoute[routeID] == nil {
			groupsByRoute[routeID] = make(map[string]*routePatternGroup)
		}
		group := groupsByRoute[routeID][key]
		if group == nil {
			group = &routePatternGroup{
				stopIDs: make([]StopID, len(stops)),
				trips:   make(map[TripID][]StopTime),
			}
			for i, st := range stops {
				group.stopIDs[i] = st.StopID
			}
			groupsByRoute[routeID][key] = group
		}
		group.trips[tripID] = stops
	}

	for routeID, groups := range groupsByRoute {
		keys := make([]string, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for i, key := range keys {
			patternID := routeID
			if len(keys) > 1 {
				patternID = RouteID(fmt.Sprintf("%s#%d", routeID, i+1))
			}
			group := groups[key]

			pattern := &RoutePattern{
				RouteID: patternID,
				Stops:   make([]RouteStop, len(group.stopIDs)),
			}
			for pos, stopID := range group.stopIDs {
				seq := pos + 1
				pattern.Stops[pos] = RouteStop{StopID: stopID, Sequence: seq}
				if !slices.Contains(idx.StopRoutes[stopID], patternID) {
					idx.StopRoutes[stopID] = append(idx.StopRoutes[stopID], patternID)
				}
				idx.StopPositionInRoute[fmt.Sprintf("%s:%s", stopID, patternID)] = seq
			}
			idx.RoutePatterns[patternID] = pattern

			tripIDs := make([]TripID, 0, len(group.trips))
			for tripID := range group.trips {
				tripIDs = append(tripIDs, tripID)
			}
			sort.Slice(tripIDs, func(a, b int) bool {
				left := group.trips[tripIDs[a]][0]
				right := group.trips[tripIDs[b]][0]
				if left.DepartureTime == right.DepartureTime {
					return tripIDs[a] < tripIDs[b]
				}
				return left.DepartureTime < right.DepartureTime
			})

			routeTrips := make([][]TripStopTime, 0, len(tripIDs))
			for _, tripID := range tripIDs {
				stops := group.trips[tripID]
				tripData := make([]TripStopTime, len(stops))
				for pos, st := range stops {
					tripStop := TripStopTime{TripID: tripID, ArrivalTime: st.ArrivalTime, DepartureTime: st.DepartureTime}
					tripData[pos] = tripStop
				}
				routeTrips = append(routeTrips, tripData)
			}
			idx.RouteTrips[patternID] = routeTrips
		}
	}

	return idx
}

func orderedStopKey(stops []StopTime) string {
	var b strings.Builder
	for i, st := range stops {
		if i > 0 {
			b.WriteByte('>')
		}
		b.WriteString(string(st.StopID))
	}
	return b.String()
}
