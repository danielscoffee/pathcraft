package raptor

import (
	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"testing"
)

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
