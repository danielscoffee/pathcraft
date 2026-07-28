package engine

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

func TestConcurrentRoutesAfterPreprocessing(t *testing.T) {
	e := New()
	if err := e.LoadOSM("../../../testdata/example.osm"); err != nil {
		t.Fatalf("LoadOSM() error = %v", err)
	}
	want, err := e.Route(RouteRequest{From: 1, To: 6, IncludeCoordinates: true})
	if err != nil {
		t.Fatalf("serial Route() error = %v", err)
	}

	const goroutines = 32
	errors := make(chan error, goroutines)
	var workers sync.WaitGroup
	workers.Add(goroutines)
	for worker := 0; worker < goroutines; worker++ {
		go func() {
			defer workers.Done()
			for iteration := 0; iteration < 20; iteration++ {
				got, err := e.Route(RouteRequest{From: 1, To: 6, IncludeCoordinates: true})
				if err != nil {
					errors <- err
					return
				}
				if !slices.Equal(got.Nodes, want.Nodes) || !slices.Equal(got.Coordinates, want.Coordinates) ||
					got.Distance != want.Distance || got.Duration != want.Duration {
					errors <- fmt.Errorf("route = %+v, want %+v", got, want)
					return
				}
				if _, err := e.RouteByCoordinates(CoordinateRouteRequest{
					FromLat: -8.05428, FromLon: -34.88130,
					ToLat: -8.05520, ToLon: -34.87970,
				}); err != nil {
					errors <- err
					return
				}
			}
		}()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Errorf("concurrent route: %v", err)
	}
}
