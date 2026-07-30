package street_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
	"github.com/danielscoffee/pathcraft/internal/routing/street"
)

func TestRouteMatchesEngineCoordinateRouting(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(1, 0, 0)
	g.AddNode(2, 0, 0.001)
	g.AddNode(3, 0, 0.002)
	segmentDistance := geo.HaversineDistance(0, 0, 0, 0.001)
	g.AddBidirectionalEdge(1, 2, segmentDistance)
	g.AddBidirectionalEdge(2, 3, segmentDistance)
	profile := mobility.NewWalking(1.4)
	got, err := street.Route(context.Background(), g, street.Request{
		FromLat:            0,
		FromLon:            0,
		ToLat:              0,
		ToLon:              0.002,
		Profile:            profile,
		IncludeCoordinates: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Nodes, []int64{1, 2, 3}) || got.Distance != 2*segmentDistance ||
		got.Duration != time.Duration(2*segmentDistance/1.4*float64(time.Second)) ||
		got.FromNodeID != 1 || got.ToNodeID != 3 || got.FromSnapDistanceM != 0 || got.ToSnapDistanceM != 0 {
		t.Fatalf("street result = %+v", got)
	}
	wantCoordinates := []street.Coordinate{{Lat: 0, Lon: 0}, {Lat: 0, Lon: 0.001}, {Lat: 0, Lon: 0.002}}
	if !reflect.DeepEqual(got.Coordinates, wantCoordinates) {
		t.Fatalf("coordinates = %+v, want %+v", got.Coordinates, wantCoordinates)
	}
}

func TestRouteHonorsProfileRestrictions(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(1, 0, 0)
	g.AddNode(2, 0, 0.001)
	g.AddRestrictedEdgeWithMeta(1, 2, 100, "footway", "", graph.RestrictedDriving)
	request := street.Request{FromLat: 0, FromLon: 0, ToLat: 0, ToLon: 0.001}

	request.Profile = mobility.NewWalking(1.4)
	if _, err := street.Route(context.Background(), g, request); err != nil {
		t.Fatalf("walking Route() error = %v", err)
	}
	request.Profile = mobility.NewDriving(8.3)
	if _, err := street.Route(context.Background(), g, request); !errors.Is(err, astar.ErrNoPath) {
		t.Fatalf("driving Route() error = %v, want ErrNoPath", err)
	}
}

func TestRouteRejectsNonFiniteInputs(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(1, 0, 0)
	for _, request := range []street.Request{
		{FromLat: math.NaN()},
		{FromLon: math.Inf(1)},
		{ToLat: math.Inf(-1)},
		{ToLon: math.NaN()},
	} {
		if _, err := street.Route(context.Background(), g, request); err == nil {
			t.Fatalf("Route(%+v) error = nil", request)
		}
	}
}

func TestRouteHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := graph.NewGraph()
	g.AddNode(1, 0, 0)
	_, err := street.Route(ctx, g, street.Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Route() error = %v, want context.Canceled", err)
	}
}
