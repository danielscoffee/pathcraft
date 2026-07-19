package gtfs_test

import (
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/internal/gtfs"
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
}

func TestParseTripInfos(t *testing.T) {
	csvData := `route_id,service_id,trip_id,trip_headsign,direction_id,shape_id
010,WD,T1,Principal,0,S1
020,WD,T2,Principal,1,S2
`

	infos, err := gtfs.ParseTripInfos(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ParseTripInfos() error = %v", err)
	}
	if infos["T1"].RouteID != "010" || infos["T1"].ShapeID != "S1" {
		t.Fatalf("unexpected T1 info: %+v", infos["T1"])
	}
}

func TestParseShapes(t *testing.T) {
	csvData := `shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence
S1,-8.0,-34.0,2
S1,-8.1,-34.1,1
S2,-9.0,-35.0,1
`

	shapes, err := gtfs.ParseShapes(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ParseShapes() error = %v", err)
	}
	if len(shapes["S1"]) != 2 {
		t.Fatalf("expected 2 S1 points, got %d", len(shapes["S1"]))
	}
	if shapes["S1"][0].Sequence != 1 || shapes["S1"][0].Lat != -8.1 {
		t.Fatalf("expected S1 sorted by sequence, got %+v", shapes["S1"])
	}
}

func TestParseRoutes(t *testing.T) {
	csvData := `route_id,agency_id,route_short_name,route_long_name,route_type
001,CTC,001,Ponte Dos Carvalhos / Prazeres,3
010,BOA,010,Abdo Cabus,3
`

	routes, err := gtfs.ParseRoutes(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ParseRoutes() error = %v", err)
	}
	if routes["001"].ShortName != "001" || routes["001"].LongName != "Ponte Dos Carvalhos / Prazeres" {
		t.Fatalf("unexpected route 001 metadata: %+v", routes["001"])
	}
}

func TestParseStops(t *testing.T) {
	csvData := `stop_id,stop_name,stop_lat,stop_lon
stopA,Stop A,-8.05428,-34.88130
stopB,Stop B,-8.05480,-34.88030
`

	stops, err := gtfs.ParseStops(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ParseStops() error = %v", err)
	}

	if len(stops) != 2 {
		t.Fatalf("expected 2 stops, got %d", len(stops))
	}

	stopA := stops["stopA"]
	if stopA.Name != "Stop A" {
		t.Errorf("stopA.Name = %q, want %q", stopA.Name, "Stop A")
	}
	if stopA.Lat != -8.05428 || stopA.Lon != -34.88130 {
		t.Errorf("stopA coords = (%v, %v), want (-8.05428, -34.88130)", stopA.Lat, stopA.Lon)
	}
}
