package raptor

import (
	"fmt"
	"testing"

	"github.com/danielscoffee/pathcraft/internal/gtfs"
	ptime "github.com/danielscoffee/pathcraft/internal/time"
)

// buildSyntheticGTFS builds a synthetic GTFS network with `routes` linear
// routes, each with `stopsPerRoute` stops and `tripsPerRoute` trips leaving
// at 1-minute intervals starting at 05:00:00. Consecutive stops on a route
// are 2 minutes apart. The LAST stop of route r is linked to the FIRST stop
// of route r+1 by a 60s bidirectional transfer, so a journey that chains all
// routes is possible (r+1 boardings to reach the last stop of route r).
//
// Returns the index, transfers, a "source" stop, and a "far" target stop.
func buildSyntheticGTFS(routes, stopsPerRoute, tripsPerRoute int) (
	*gtfs.StopTimeIndex,
	map[gtfs.StopID][]Transfer,
	gtfs.StopID,
	gtfs.StopID,
) {
	var stopTimes []gtfs.StopTime
	tripRoutes := gtfs.TripToRoute{}

	const baseTime ptime.Time = 5 * 3600 // 05:00:00
	const dwellSec ptime.Time = 30
	const hopSec ptime.Time = 120

	stopID := func(r, s int) gtfs.StopID {
		return gtfs.StopID(fmt.Sprintf("R%d_S%d", r, s))
	}

	for r := 0; r < routes; r++ {
		routeID := gtfs.RouteID(fmt.Sprintf("RT%d", r))
		for t := 0; t < tripsPerRoute; t++ {
			tripID := gtfs.TripID(fmt.Sprintf("T%d_%d", r, t))
			tripRoutes[tripID] = routeID
			start := baseTime + ptime.Time(t)*60
			for s := 0; s < stopsPerRoute; s++ {
				arr := start + ptime.Time(s)*hopSec
				dep := arr + dwellSec
				stopTimes = append(stopTimes, gtfs.StopTime{
					TripID:        tripID,
					StopID:        stopID(r, s),
					ArrivalTime:   arr,
					DepartureTime: dep,
					StopSequence:  s + 1,
				})
			}
		}
	}

	idx := gtfs.BuildIndex(stopTimes, tripRoutes)

	// Transfers: last stop of route r <-> first stop of route r+1, 60s walk.
	transfers := map[gtfs.StopID][]Transfer{}
	for r := 0; r < routes-1; r++ {
		a := stopID(r, stopsPerRoute-1)
		b := stopID(r+1, 0)
		transfers[a] = append(transfers[a], Transfer{To: b, Duration: 60})
		transfers[b] = append(transfers[b], Transfer{To: a, Duration: 60})
	}

	source := stopID(0, 0)
	target := stopID(routes-1, stopsPerRoute-1)
	return idx, transfers, source, target
}

// BenchmarkRAPTOR_Search measures pure in-memory RAPTOR earliest-arrival
// search on a preloaded synthetic GTFS index. It excludes parse, index build,
// and path reconstruction.
func BenchmarkRAPTOR_Search(b *testing.B) {
	cases := []struct {
		routes, stops, trips int
	}{
		{5, 10, 10},    // toy
		{20, 20, 30},   // small network
		{50, 30, 60},   // mid network (~Recife-ish order of magnitude)
		{100, 40, 100}, // large network
	}

	const departure ptime.Time = 5 * 3600

	for _, c := range cases {
		c := c
		name := fmt.Sprintf("routes=%d_stopsPerRoute=%d_trips=%d",
			c.routes, c.stops, c.trips)
		b.Run(name, func(b *testing.B) {
			idx, transfers, src, _ := buildSyntheticGTFS(c.routes, c.stops, c.trips)
			router := NewRouter(idx, transfers)

			// Warm-up
			if res := router.Search(src, departure); res == nil {
				b.Fatal("nil result in warmup")
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				res := router.Search(src, departure)
				if res == nil {
					b.Fatal("nil result")
				}
			}
		})
	}
}

// BenchmarkRAPTOR_SearchAndReconstruct includes path reconstruction to the
// far target — closer to what a user-facing transit query returns.
func BenchmarkRAPTOR_SearchAndReconstruct(b *testing.B) {
	// 2-route network: one boarding + one transfer + one boarding.
	// This is a realistic "journey with 1 transfer" query and avoids the
	// schedule-coverage issue of long chained-transfer synthetic networks.
	idx, transfers, src, _ := buildSyntheticGTFS(2, 30, 60)
	router := NewRouter(idx, transfers)
	dst := gtfs.StopID("R1_S29")

	// Warm-up
	if res := router.Search(src, 5*3600); res == nil || res.ReconstructPath(dst) == nil {
		b.Fatal("warmup: unreachable target")
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		res := router.Search(src, 5*3600)
		if res == nil {
			b.Fatal("nil result")
		}
		path := res.ReconstructPath(dst)
		if path == nil {
			b.Fatal("unreachable target")
		}
	}
}

// BenchmarkRAPTOR_ToyExistingFixture replicates the fixture in raptor_test.go
// (5 stop_times, 2 trips, 1 transfer) to provide a ceiling-best "toy" number
// for honest comparison with ad-hoc CLI prints.
func BenchmarkRAPTOR_ToyExistingFixture(b *testing.B) {
	stopTimes := []gtfs.StopTime{
		{TripID: "T1", StopID: "A", ArrivalTime: 100, DepartureTime: 110, StopSequence: 1},
		{TripID: "T1", StopID: "B", ArrivalTime: 200, DepartureTime: 210, StopSequence: 2},
		{TripID: "T1", StopID: "C", ArrivalTime: 300, DepartureTime: 310, StopSequence: 3},
		{TripID: "T2", StopID: "B", ArrivalTime: 250, DepartureTime: 260, StopSequence: 1},
		{TripID: "T2", StopID: "D", ArrivalTime: 400, DepartureTime: 410, StopSequence: 2},
	}
	tripRoutes := gtfs.TripToRoute{"T1": "R1", "T2": "R2"}
	idx := gtfs.BuildIndex(stopTimes, tripRoutes)
	transfers := map[gtfs.StopID][]Transfer{"C": {{To: "D", Duration: 50}}}
	router := NewRouter(idx, transfers)

	if router.Search("A", 0) == nil {
		b.Fatal("warmup failed")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = router.Search("A", 0)
	}
}
