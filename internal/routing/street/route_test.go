package street_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
	"github.com/danielscoffee/pathcraft/internal/routing/street"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

const routeOSM = `<osm version="0.6">
  <node id="1" lat="0" lon="0"/>
  <node id="2" lat="0" lon="0.001"/>
  <node id="3" lat="0" lon="0.002"/>
  <way id="10">
    <nd ref="1"/><nd ref="2"/><nd ref="3"/>
    <tag k="highway" v="residential"/>
  </way>
</osm>`

func TestRouteMatchesEngineCoordinateRouting(t *testing.T) {
	legacy := engine.New()
	if err := legacy.LoadOSMReader(strings.NewReader(routeOSM)); err != nil {
		t.Fatal(err)
	}
	profile := mobility.NewWalking(1.4)
	want, err := legacy.RouteByCoordinates(engine.CoordinateRouteRequest{
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
	got, err := street.Route(context.Background(), legacy.GetGraph(), street.Request{
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
	if !reflect.DeepEqual(got.Nodes, want.Nodes) || got.Distance != want.Distance || got.Duration != want.Duration ||
		got.FromNodeID != want.FromNodeID || got.ToNodeID != want.ToNodeID ||
		got.FromSnapDistanceM != want.FromSnapDistanceM || got.ToSnapDistanceM != want.ToSnapDistanceM {
		t.Fatalf("street result = %+v, engine result = %+v", got, want)
	}
	if len(got.Coordinates) != len(want.Coordinates) {
		t.Fatalf("coordinate count = %d, want %d", len(got.Coordinates), len(want.Coordinates))
	}
	for index := range got.Coordinates {
		if got.Coordinates[index].Lat != want.Coordinates[index].Lat || got.Coordinates[index].Lon != want.Coordinates[index].Lon {
			t.Fatalf("coordinate %d = %+v, want %+v", index, got.Coordinates[index], want.Coordinates[index])
		}
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
