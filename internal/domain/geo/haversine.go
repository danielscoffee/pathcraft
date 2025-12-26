package geo

import (
	"math"

	"github.com/qoppatech/exp-pathcraft/internal/domain/graph"
)

type Heuristic func(from, to graph.Node) float64

// HaversineHeuristic returns walking time estimate based on straight line distance.
func HaversineHeuristic(walkingSpeedMPS float64) Heuristic {
	return func(from, to graph.Node) float64 {
		dist := HaversineDistance(from.Lat, from.Lon, to.Lat, to.Lon)
		return dist / walkingSpeedMPS
	}
}

// HaversineDistance calculates the great circle distance in meters.
func HaversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	lat1Rad := lat1 * DegreesToRadians
	lat2Rad := lat2 * DegreesToRadians
	deltaLat := (lat2 - lat1) * DegreesToRadians
	deltaLon := (lon2 - lon1) * DegreesToRadians

	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return EarthRadiusMeters * c
}
