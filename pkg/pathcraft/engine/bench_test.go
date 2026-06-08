package engine

import (
	"testing"

	"github.com/danielscoffee/pathcraft/internal/mobility"
)

// BenchmarkEngine_Route_ExampleOSM measures an end-to-end node-to-node
// walking query via the public Engine facade (LoadOSM excluded from the
// timed loop, graph is preloaded). Uses the example.osm toy fixture.
//
// This is what the HTTP /route?from=<id>&to=<id> endpoint does internally,
// minus HTTP/GeoJSON overhead.
func BenchmarkEngine_Route_ExampleOSM(b *testing.B) {
	e := New()
	if err := e.LoadOSM("../../../examples/example.osm"); err != nil {
		b.Fatalf("LoadOSM: %v", err)
	}
	profile := mobility.NewWalking(1.4)
	req := RouteRequest{From: 1, To: 6, Profile: profile, IncludeCoordinates: false}

	// Warm-up
	if _, err := e.Route(req); err != nil {
		b.Fatalf("warmup Route: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Route(req); err != nil {
			b.Fatalf("Route: %v", err)
		}
	}
}

// BenchmarkEngine_RouteByCoordinates_ExampleOSM measures the coordinate-based
// path: nearest-node snap (start) + nearest-node snap (end) + A*. Still on
// the toy fixture; numbers are a lower bound.
func BenchmarkEngine_RouteByCoordinates_ExampleOSM(b *testing.B) {
	e := New()
	if err := e.LoadOSM("../../../examples/example.osm"); err != nil {
		b.Fatalf("LoadOSM: %v", err)
	}
	profile := mobility.NewWalking(1.4)
	req := CoordinateRouteRequest{
		FromLat: -8.05428, FromLon: -34.88130,
		ToLat: -8.05480, ToLon: -34.88030,
		Profile: profile,
	}

	if _, err := e.RouteByCoordinates(req); err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.RouteByCoordinates(req); err != nil {
			b.Fatalf("RouteByCoordinates: %v", err)
		}
	}
}

// BenchmarkEngine_TransitRoute_ExampleGTFS measures end-to-end transit
// routing via the public facade on the Recife example GTFS fixture
// (~102 stop_times, 16 trips, 6 transfers).
func BenchmarkEngine_TransitRoute_ExampleGTFS(b *testing.B) {
	e := New()
	if err := e.LoadGTFSDir("../../../examples/gtfs"); err != nil {
		b.Fatalf("LoadGTFSDir: %v", err)
	}
	req := TransitRouteRequest{
		FromStop:      "RECIFE",
		ToStop:        "TI_CDU",
		DepartureTime: "05:00:00",
	}
	if _, err := e.TransitRoute(req); err != nil {
		b.Fatalf("warmup TransitRoute: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.TransitRoute(req); err != nil {
			b.Fatalf("TransitRoute: %v", err)
		}
	}
}
