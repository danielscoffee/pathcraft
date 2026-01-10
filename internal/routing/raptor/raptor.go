package raptor

import (
	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/time"
)

type JourneyStep struct {
	FromStop   gtfs.StopID
	ToStop     gtfs.StopID
	TripID     gtfs.TripID // Empty if transfer
	IsTransfer bool
}

type Result struct {
	ArrivalTimes    []map[gtfs.StopID]time.Time
	EarliestArrival map[gtfs.StopID]time.Time
	Parents         []map[gtfs.StopID]JourneyStep
}

type Transfer struct {
	To       gtfs.StopID
	Duration time.Time
}

type Router struct {
	index     *gtfs.StopTimeIndex
	transfers map[gtfs.StopID][]Transfer
}

func NewRouter(index *gtfs.StopTimeIndex, transfers map[gtfs.StopID][]Transfer) *Router {
	return &Router{
		index:     index,
		transfers: transfers,
	}
}

const MaxRounds = 10

func (r *Router) Search(source gtfs.StopID, departureTime time.Time) *Result {
	earliestArrival := make(map[gtfs.StopID]time.Time)
	arrivalTimes := make([]map[gtfs.StopID]time.Time, MaxRounds+1)
	parents := make([]map[gtfs.StopID]JourneyStep, MaxRounds+1)

	for k := 0; k <= MaxRounds; k++ {
		arrivalTimes[k] = make(map[gtfs.StopID]time.Time)
		parents[k] = make(map[gtfs.StopID]JourneyStep)
	}

	arrivalTimes[0][source] = departureTime
	earliestArrival[source] = departureTime

	markedStops := make(map[gtfs.StopID]bool)
	markedStops[source] = true

	for k := 1; k <= MaxRounds; k++ {
		for stop, t := range arrivalTimes[k-1] {
			arrivalTimes[k][stop] = t
		}

		activeRoutes := make(map[gtfs.RouteID]int)
		for stopID := range markedStops {
			routes := r.index.RoutesAtStop(stopID)
			for _, routeID := range routes {
				seq := r.index.GetStopSequence(stopID, routeID)
				if currentSeq, ok := activeRoutes[routeID]; !ok || seq < currentSeq {
					activeRoutes[routeID] = seq
				}
			}
		}

		markedStops = make(map[gtfs.StopID]bool)

		for routeID, startSeq := range activeRoutes {
			tripIndex := -1
			var boardingStop gtfs.StopID
			pattern := r.index.RoutePatterns[routeID]
			routeTrips := r.index.RouteTrips[routeID]

			startStopIndex := -1
			for i, s := range pattern.Stops {
				if s.Sequence == startSeq {
					startStopIndex = i
					break
				}
			}

			if startStopIndex == -1 {
				continue
			}

			for i := startStopIndex; i < len(pattern.Stops); i++ {
				stop := pattern.Stops[i]

				if tripIndex != -1 {
					trip := routeTrips[tripIndex][i]
					arrTime := trip.ArrivalTime

					if existing, ok := earliestArrival[stop.StopID]; !ok || arrTime < existing {
						arrivalTimes[k][stop.StopID] = arrTime
						earliestArrival[stop.StopID] = arrTime
						markedStops[stop.StopID] = true
						parents[k][stop.StopID] = JourneyStep{
							FromStop: boardingStop,
							ToStop:   stop.StopID,
							TripID:   trip.TripID,
						}
					}
				}

				// Can we catch a better trip at this stop?
				if prevArrival, ok := arrivalTimes[k-1][stop.StopID]; ok {
					etIndex := r.index.EarliestTripIndex(routeID, i, prevArrival)
					if etIndex != -1 {
						if tripIndex == -1 || etIndex < tripIndex {
							tripIndex = etIndex
							boardingStop = stop.StopID
						}
					}
				}
			}
		}

		// Handle transfers
		// We need to iterate over stops updated in THIS round (Step 2)
		updatedInStep2 := make([]gtfs.StopID, 0, len(markedStops))
		for stopID := range markedStops {
			updatedInStep2 = append(updatedInStep2, stopID)
		}

		for _, stopID := range updatedInStep2 {
			for _, tr := range r.transfers[stopID] {
				arrWithTransfer := arrivalTimes[k][stopID] + tr.Duration
				if existing, ok := earliestArrival[tr.To]; !ok || arrWithTransfer < existing {
					arrivalTimes[k][tr.To] = arrWithTransfer
					earliestArrival[tr.To] = arrWithTransfer
					markedStops[tr.To] = true
					parents[k][tr.To] = JourneyStep{
						FromStop:   stopID,
						ToStop:     tr.To,
						IsTransfer: true,
					}
				}
			}
		}

		if len(markedStops) == 0 {
			break
		}
	}

	return &Result{
		ArrivalTimes:    arrivalTimes,
		EarliestArrival: earliestArrival,
		Parents:         parents,
	}
}

func (res *Result) ReconstructPath(target gtfs.StopID) []JourneyStep {
	path := []JourneyStep{}
	currentStop := target

	bestRound := -1
	var bestTime time.Time

	for k, arrivals := range res.ArrivalTimes {
		if t, ok := arrivals[target]; ok {
			if bestRound == -1 || t < bestTime {
				bestTime = t
				bestRound = k
			}
		}
	}

	if bestRound == -1 {
		return nil
	}

	for k := bestRound; k > 0; {
		step, ok := res.Parents[k][currentStop]
		if !ok {
			break
		}
		path = append(path, step)
		currentStop = step.FromStop

		if !step.IsTransfer {
			k--
		}
	}

	// Reverse path
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	return path
}
