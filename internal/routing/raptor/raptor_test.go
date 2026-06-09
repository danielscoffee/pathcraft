package raptor

import (
	"testing"

	"github.com/danielscoffee/pathcraft/internal/gtfs"
)

func TestRAPTORHandlesOppositeDirectionsOnSameRoute(t *testing.T) {
	stopTimes := []gtfs.StopTime{
		{TripID: "OUT", StopID: "A", ArrivalTime: 8 * 3600, DepartureTime: 8 * 3600, StopSequence: 1},
		{TripID: "OUT", StopID: "B", ArrivalTime: 8*3600 + 300, DepartureTime: 8*3600 + 300, StopSequence: 2},
		{TripID: "OUT", StopID: "C", ArrivalTime: 8*3600 + 600, DepartureTime: 8*3600 + 600, StopSequence: 3},
		{TripID: "BACK", StopID: "C", ArrivalTime: 9 * 3600, DepartureTime: 9 * 3600, StopSequence: 1},
		{TripID: "BACK", StopID: "B", ArrivalTime: 9*3600 + 300, DepartureTime: 9*3600 + 300, StopSequence: 2},
		{TripID: "BACK", StopID: "A", ArrivalTime: 9*3600 + 600, DepartureTime: 9*3600 + 600, StopSequence: 3},
	}
	idx := gtfs.BuildIndex(stopTimes, gtfs.TripToRoute{"OUT": "R", "BACK": "R"})
	router := NewRouter(idx, nil)

	out := router.Search("A", 7*3600)
	if got, ok := out.EarliestArrival["C"]; !ok || got != 8*3600+600 {
		t.Fatalf("A -> C arrival = %v, %v; want %v, true", got, ok, 8*3600+600)
	}

	back := router.Search("C", 8*3600+1800)
	if got, ok := back.EarliestArrival["A"]; !ok || got != 9*3600+600 {
		t.Fatalf("C -> A arrival = %v, %v; want %v, true", got, ok, 9*3600+600)
	}
}

func TestRAPTOR(t *testing.T) {
	// Setup a simple network
	// Stop A -> Stop B -> Stop C (Route 1)
	// Stop B -> Stop D (Route 2)
	// Stop C -> Stop D (Transfer)

	stopTimes := []gtfs.StopTime{
		// Route 1, Trip 1
		{TripID: "T1", StopID: "A", ArrivalTime: 100, DepartureTime: 110, StopSequence: 1},
		{TripID: "T1", StopID: "B", ArrivalTime: 200, DepartureTime: 210, StopSequence: 2},
		{TripID: "T1", StopID: "C", ArrivalTime: 300, DepartureTime: 310, StopSequence: 3},
		// Route 2, Trip 2
		{TripID: "T2", StopID: "B", ArrivalTime: 250, DepartureTime: 260, StopSequence: 1},
		{TripID: "T2", StopID: "D", ArrivalTime: 400, DepartureTime: 410, StopSequence: 2},
	}

	tripRoutes := gtfs.TripToRoute{
		"T1": "R1",
		"T2": "R2",
	}

	idx := gtfs.BuildIndex(stopTimes, tripRoutes)
	transfers := map[gtfs.StopID][]Transfer{
		"C": {{To: "D", Duration: 50}},
	}

	router := NewRouter(idx, transfers)

	// Search from A at time 0
	res := router.Search("A", 0)

	if res == nil {
		t.Fatal("Result is nil")
	}

	// Check arrival at D
	// Option 1: A -> B -> D (2 trips)
	// T1 to B (arr 200), T2 from B (dep 260, arr 400)
	// Option 2: A -> B -> C -> D (1 trip + transfer)
	// T1 to C (arr 300), transfer to D (arr 300+50=350)

	arrD, ok := res.EarliestArrival["D"]
	if !ok {
		t.Error("Stop D not reached")
	} else if arrD != 350 {
		t.Errorf("Expected arrival at D to be 350, got %d", arrD)
	}

	path := res.ReconstructPath("D")
	if len(path) == 0 {
		t.Error("Path to D not found")
	}

	t.Logf("Path to D: %+v", path)
}
