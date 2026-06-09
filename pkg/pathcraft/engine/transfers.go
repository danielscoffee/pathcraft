package engine

import (
	"time"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
)

func (e *Engine) transitLegCoordinates(tripID gtfs.TripID, fromStopID, toStopID gtfs.StopID) []Coordinate {
	if coords := e.shapeLegCoordinates(tripID, fromStopID, toStopID); len(coords) > 0 {
		return coords
	}

	stopTimes, err := e.GTFSTripStopTimes(string(tripID))
	if err != nil {
		return e.stopPairCoordinates(fromStopID, toStopID)
	}
	fromIdx, toIdx := -1, -1
	for i, st := range stopTimes {
		if gtfs.StopID(st.StopID) == fromStopID && fromIdx == -1 {
			fromIdx = i
		}
		if gtfs.StopID(st.StopID) == toStopID {
			toIdx = i
		}
	}
	if fromIdx == -1 || toIdx == -1 {
		return e.stopPairCoordinates(fromStopID, toStopID)
	}
	if fromIdx > toIdx {
		fromIdx, toIdx = toIdx, fromIdx
	}
	coords := make([]Coordinate, 0, toIdx-fromIdx+1)
	for _, st := range stopTimes[fromIdx : toIdx+1] {
		coords = append(coords, Coordinate{Lat: st.Lat, Lon: st.Lon})
	}
	return coords
}

func (e *Engine) shapeLegCoordinates(tripID gtfs.TripID, fromStopID, toStopID gtfs.StopID) []Coordinate {
	shapeID, ok := e.gtfsTripShapes[tripID]
	if !ok || shapeID == "" {
		return nil
	}
	shape := e.gtfsShapes[shapeID]
	if len(shape) == 0 {
		return nil
	}
	fromStop, okFrom := e.gtfsStops[fromStopID]
	toStop, okTo := e.gtfsStops[toStopID]
	if !okFrom || !okTo {
		return nil
	}
	fromIdx := nearestShapePointIndex(shape, fromStop.Lat, fromStop.Lon)
	toIdx := nearestShapePointIndex(shape, toStop.Lat, toStop.Lon)
	if fromIdx < 0 || toIdx < 0 {
		return nil
	}
	reverse := false
	if fromIdx > toIdx {
		fromIdx, toIdx = toIdx, fromIdx
		reverse = true
	}
	coords := make([]Coordinate, 0, toIdx-fromIdx+1)
	for _, point := range shape[fromIdx : toIdx+1] {
		coords = append(coords, Coordinate{Lat: point.Lat, Lon: point.Lon})
	}
	if reverse {
		for i, j := 0, len(coords)-1; i < j; i, j = i+1, j-1 {
			coords[i], coords[j] = coords[j], coords[i]
		}
	}
	return coords
}

func nearestShapePointIndex(points []gtfs.ShapePoint, lat, lon float64) int {
	best := -1
	bestDist := 0.0
	for i, point := range points {
		d := geo.HaversineDistance(lat, lon, point.Lat, point.Lon)
		if best == -1 || d < bestDist {
			best = i
			bestDist = d
		}
	}
	return best
}

func (e *Engine) stopPairCoordinates(fromStopID, toStopID gtfs.StopID) []Coordinate {
	fromStop, okFrom := e.gtfsStops[fromStopID]
	toStop, okTo := e.gtfsStops[toStopID]
	if !okFrom || !okTo {
		return nil
	}
	return []Coordinate{{Lat: fromStop.Lat, Lon: fromStop.Lon}, {Lat: toStop.Lat, Lon: toStop.Lon}}
}

func coordinateLineDistance(coords []Coordinate) float64 {
	if len(coords) < 2 {
		return 0
	}
	total := 0.0
	for i := 1; i < len(coords); i++ {
		total += geo.HaversineDistance(coords[i-1].Lat, coords[i-1].Lon, coords[i].Lat, coords[i].Lon)
	}
	return total
}

func inferNearbyStopTransfers(stops map[gtfs.StopID]gtfs.Stop, maxDistanceM float64, minSeconds int) map[gtfs.StopID][]raptor.Transfer {
	out := make(map[gtfs.StopID][]raptor.Transfer)
	list := make([]gtfs.Stop, 0, len(stops))
	for _, stop := range stops {
		list = append(list, stop)
	}
	for i, from := range list {
		for j, to := range list {
			if i == j {
				continue
			}
			d := geo.HaversineDistance(from.Lat, from.Lon, to.Lat, to.Lon)
			if d > maxDistanceM {
				continue
			}
			seconds := int(d / mobility.DefaultWalkingSpeedMPS)
			if seconds < minSeconds {
				seconds = minSeconds
			}
			out[from.ID] = append(out[from.ID], raptor.Transfer{To: to.ID, Duration: pcTime.Time(seconds)})
		}
	}
	return out
}

func (e *Engine) routeShortLabel(id gtfs.RouteID) string {
	if route, ok := e.gtfsRoutes[id]; ok && route.ShortName != "" {
		return route.ShortName
	}
	return string(id)
}

func (e *Engine) routeLongLabel(id gtfs.RouteID) string {
	if route, ok := e.gtfsRoutes[id]; ok && route.LongName != "" {
		return route.LongName
	}
	return ""
}

func (e *Engine) stopLabel(id gtfs.StopID) string {
	if stop, ok := e.gtfsStops[id]; ok && stop.Name != "" {
		return stop.Name
	}
	return string(id)
}

func addDuration(t pcTime.Time, d time.Duration) pcTime.Time {
	return t + pcTime.Time(d/time.Second)
}

func durationBetween(from, to pcTime.Time) time.Duration {
	return time.Duration(int(to-from)) * time.Second
}
