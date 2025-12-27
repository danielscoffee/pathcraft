package gtfs_test

import (
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/time"
)

func TestParseStopTimes(t *testing.T) {
	csvData := `trip_id,arrival_time,departure_time,stop_id,stop_sequence
trip1,08:00:00,08:00:00,stopA,1
trip1,08:10:00,08:11:00,stopB,2
trip1,08:20:00,08:20:00,stopC,3
trip2,09:00:00,09:00:00,stopA,1
trip2,09:15:00,09:16:00,stopB,2
`

	stopTimes, err := gtfs.ParseStopTimes(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ParseStopTimes() error = %v", err)
	}

	if len(stopTimes) != 5 {
		t.Errorf("expected 5 stop times, got %d", len(stopTimes))
	}

	first := stopTimes[0]
	if first.TripID != "trip1" {
		t.Errorf("first.TripID = %q, want %q", first.TripID, "trip1")
	}
	if first.StopID != "stopA" {
		t.Errorf("first.StopID = %q, want %q", first.StopID, "stopA")
	}
	if first.ArrivalTime != 8*3600 {
		t.Errorf("first.ArrivalTime = %d, want %d", first.ArrivalTime, 8*3600)
	}
	if first.StopSequence != 1 {
		t.Errorf("first.StopSequence = %d, want %d", first.StopSequence, 1)
	}
}

func TestParseStopTimes_MissingColumn(t *testing.T) {
	csvData := `trip_id,arrival_time,stop_id,stop_sequence
trip1,08:00:00,stopA,1
`
	_, err := gtfs.ParseStopTimes(strings.NewReader(csvData))
	if err == nil {
		t.Error("expected error for missing column, got nil")
	}
}

func TestBuildIndex(t *testing.T) {
	stopTimes := []gtfs.StopTime{
		{TripID: "trip1", StopID: "stopA", ArrivalTime: 8 * 3600, DepartureTime: 8 * 3600, StopSequence: 1},
		{TripID: "trip1", StopID: "stopB", ArrivalTime: 8*3600 + 600, DepartureTime: 8*3600 + 660, StopSequence: 2},
		{TripID: "trip1", StopID: "stopC", ArrivalTime: 8*3600 + 1200, DepartureTime: 8*3600 + 1200, StopSequence: 3},
		{TripID: "trip2", StopID: "stopA", ArrivalTime: 9 * 3600, DepartureTime: 9 * 3600, StopSequence: 1},
		{TripID: "trip2", StopID: "stopB", ArrivalTime: 9*3600 + 900, DepartureTime: 9*3600 + 960, StopSequence: 2},
		{TripID: "trip2", StopID: "stopC", ArrivalTime: 9*3600 + 1800, DepartureTime: 9*3600 + 1800, StopSequence: 3},
	}

	tripRoutes := gtfs.TripToRoute{
		"trip1": "routeR",
		"trip2": "routeR",
	}

	idx := gtfs.BuildIndex(stopTimes, tripRoutes)

	routes := idx.RoutesAtStop("stopA")
	if len(routes) != 1 || routes[0] != "routeR" {
		t.Errorf("RoutesAtStop(stopA) = %v, want [routeR]", routes)
	}

	stops := idx.StopsOnRoute("routeR")
	if len(stops) != 3 {
		t.Errorf("StopsOnRoute(routeR) has %d stops, want 3", len(stops))
	}

	seq := idx.GetStopSequence("stopB", "routeR")
	if seq != 2 {
		t.Errorf("GetStopSequence(stopB, routeR) = %d, want 2", seq)
	}

	trips := idx.TripsAtRouteStop("routeR", 1)
	if len(trips) != 2 {
		t.Errorf("TripsAtRouteStop(routeR, 1) has %d trips, want 2", len(trips))
	}
	if trips[0].TripID != "trip1" {
		t.Errorf("first trip should be trip1 (earlier), got %s", trips[0].TripID)
	}
}

func TestEarliestTrip(t *testing.T) {
	stopTimes := []gtfs.StopTime{
		{TripID: "trip1", StopID: "stopA", ArrivalTime: 8 * 3600, DepartureTime: 8 * 3600, StopSequence: 1},
		{TripID: "trip2", StopID: "stopA", ArrivalTime: 9 * 3600, DepartureTime: 9 * 3600, StopSequence: 1},
		{TripID: "trip3", StopID: "stopA", ArrivalTime: 10 * 3600, DepartureTime: 10 * 3600, StopSequence: 1},
	}

	tripRoutes := gtfs.TripToRoute{
		"trip1": "routeR",
		"trip2": "routeR",
		"trip3": "routeR",
	}

	idx := gtfs.BuildIndex(stopTimes, tripRoutes)

	tests := []struct {
		minTime  time.Time
		expected gtfs.TripID
	}{
		{7 * 3600, "trip1"},
		{8 * 3600, "trip1"},
		{8*3600 + 1, "trip2"},
		{9*3600 + 30*60, "trip3"},
		{11 * 3600, ""},
	}

	for _, tt := range tests {
		trip := idx.EarliestTrip("routeR", 1, tt.minTime)
		if tt.expected == "" {
			if trip != nil {
				t.Errorf("EarliestTrip(minTime=%v) = %v, want nil", tt.minTime, trip.TripID)
			}
		} else {
			if trip == nil {
				t.Errorf("EarliestTrip(minTime=%v) = nil, want %s", tt.minTime, tt.expected)
			} else if trip.TripID != tt.expected {
				t.Errorf("EarliestTrip(minTime=%v) = %s, want %s", tt.minTime, trip.TripID, tt.expected)
			}
		}
	}
}
