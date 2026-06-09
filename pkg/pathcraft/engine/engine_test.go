package engine

import (
	"encoding/json"
	"testing"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
)

func buildRoutingGraph() *graph.Graph {
	g := graph.NewGraph()
	g.AddNode(1, -8.05428, -34.88130)
	g.AddNode(2, -8.05428, -34.88080)
	g.AddNode(4, -8.05450, -34.88080)
	g.AddNode(5, -8.05480, -34.88080)
	g.AddNode(6, -8.05480, -34.88030)
	g.AddBidirectionalEdge(1, 2, geo.HaversineDistance(-8.05428, -34.88130, -8.05428, -34.88080))
	g.AddBidirectionalEdge(2, 4, geo.HaversineDistance(-8.05428, -34.88080, -8.05450, -34.88080))
	g.AddBidirectionalEdge(4, 5, geo.HaversineDistance(-8.05450, -34.88080, -8.05480, -34.88080))
	g.AddBidirectionalEdge(5, 6, geo.HaversineDistance(-8.05480, -34.88080, -8.05480, -34.88030))
	return g
}

func TestRouteGeoJSON(t *testing.T) {
	e := New()
	g := graph.NewGraph()
	g.AddNode(1, 10, 20)
	g.AddNode(2, 10, 21)
	g.AddBidirectionalEdge(1, 2, 100)
	e.graph = g

	b, err := e.RouteGeoJSON(RouteRequest{
		From:    1,
		To:      2,
		Profile: mobility.NewWalking(1.4),
	})
	if err != nil {
		t.Fatalf("RouteGeoJSON() error = %v", err)
	}

	var doc struct {
		Type     string `json:"type"`
		Features []struct {
			Type string `json:"type"`
		} `json:"features"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if doc.Type != "FeatureCollection" {
		t.Fatalf("type = %q, want FeatureCollection", doc.Type)
	}
	if len(doc.Features) != 1 {
		t.Fatalf("features = %d, want 1", len(doc.Features))
	}
}

func TestTransitRouteRequiresReachableTarget(t *testing.T) {
	e := New()
	stopTimes := []gtfs.StopTime{{TripID: "trip1", StopID: "A", ArrivalTime: 8 * 3600, DepartureTime: 8 * 3600, StopSequence: 1}}
	tripRoutes := gtfs.TripToRoute{"trip1": "route1"}
	e.gtfsIndex = gtfs.BuildIndex(stopTimes, tripRoutes)

	_, err := e.TransitRoute(TransitRouteRequest{
		FromStop:      "A",
		ToStop:        "B",
		DepartureTime: "08:00:00",
	})
	if err == nil {
		t.Fatal("expected error for unreachable target stop")
	}
}

func TestRouteByCoordinates(t *testing.T) {
	e := New()
	e.graph = buildRoutingGraph()

	res, err := e.RouteByCoordinates(CoordinateRouteRequest{
		FromLat:            -8.05428,
		FromLon:            -34.88130,
		ToLat:              -8.05480,
		ToLon:              -34.88030,
		Profile:            mobility.NewWalking(1.4),
		IncludeCoordinates: true,
	})
	if err != nil {
		t.Fatalf("RouteByCoordinates() error = %v", err)
	}
	if res.FromNodeID != 1 || res.ToNodeID != 6 {
		t.Fatalf("unexpected snapped nodes: %d -> %d", res.FromNodeID, res.ToNodeID)
	}
	if len(res.Nodes) == 0 {
		t.Fatal("expected route nodes")
	}
}

func TestMultimodalRoutePrefersTransitWhenFaster(t *testing.T) {
	e := New()
	e.graph = buildRoutingGraph()
	e.gtfsStops = map[gtfs.StopID]gtfs.Stop{
		"START_STOP": {ID: "START_STOP", Name: "Start Stop", Lat: -8.05428, Lon: -34.88080},
		"MID_STOP":   {ID: "MID_STOP", Name: "Mid Stop", Lat: -8.05450, Lon: -34.88080},
		"END_STOP":   {ID: "END_STOP", Name: "End Stop", Lat: -8.05480, Lon: -34.88080},
	}
	e.gtfsTransfers = map[gtfs.StopID][]raptor.Transfer{}
	e.gtfsIndex = gtfs.BuildIndex([]gtfs.StopTime{
		{TripID: "SHUTTLE_1", StopID: "START_STOP", ArrivalTime: 5*3600 + 45, DepartureTime: 5*3600 + 45, StopSequence: 1},
		{TripID: "SHUTTLE_1", StopID: "MID_STOP", ArrivalTime: 5*3600 + 55, DepartureTime: 5*3600 + 55, StopSequence: 2},
		{TripID: "SHUTTLE_1", StopID: "END_STOP", ArrivalTime: 5*3600 + 65, DepartureTime: 5*3600 + 65, StopSequence: 3},
	}, gtfs.TripToRoute{"SHUTTLE_1": "SHUTTLE"})
	e.gtfsTripShapes = map[gtfs.TripID]gtfs.ShapeID{"SHUTTLE_1": "SHAPE_1"}
	e.gtfsShapes = map[gtfs.ShapeID][]gtfs.ShapePoint{"SHAPE_1": {
		{ShapeID: "SHAPE_1", Lat: -8.05428, Lon: -34.88080, Sequence: 1},
		{ShapeID: "SHAPE_1", Lat: -8.05435, Lon: -34.88070, Sequence: 2},
		{ShapeID: "SHAPE_1", Lat: -8.05450, Lon: -34.88060, Sequence: 3},
		{ShapeID: "SHAPE_1", Lat: -8.05465, Lon: -34.88070, Sequence: 4},
		{ShapeID: "SHAPE_1", Lat: -8.05480, Lon: -34.88080, Sequence: 5},
	}}

	res, err := e.MultimodalRoute(MultimodalRouteRequest{
		FromLat:        -8.05428,
		FromLon:        -34.88130,
		ToLat:          -8.05480,
		ToLon:          -34.88030,
		DepartureTime:  "05:00:00",
		WalkingProfile: mobility.NewWalking(1.4),
	})
	if err != nil {
		t.Fatalf("MultimodalRoute() error = %v", err)
	}
	if res.Mode != "multimodal" {
		t.Fatalf("mode = %q, want multimodal", res.Mode)
	}
	if len(res.Legs) < 3 {
		t.Fatalf("expected walk/transit/walk legs, got %d", len(res.Legs))
	}
	if res.OriginStopID != "START_STOP" || res.DestinationStopID != "END_STOP" {
		t.Fatalf("unexpected transit stops: %s -> %s", res.OriginStopID, res.DestinationStopID)
	}
	var transitLeg *JourneyLeg
	for i := range res.Legs {
		if res.Legs[i].Mode == "transit" {
			transitLeg = &res.Legs[i]
			break
		}
	}
	if transitLeg == nil {
		t.Fatal("expected a transit leg")
	}
	if len(transitLeg.Coordinates) != 5 {
		t.Fatalf("transit leg coordinates = %v, want GTFS shape polyline", transitLeg.Coordinates)
	}
	if transitLeg.Coordinates[2].Lat != -8.05450 || transitLeg.Coordinates[2].Lon != -34.88060 {
		t.Fatalf("expected shape coordinate in transit geometry, got %+v", transitLeg.Coordinates[2])
	}
}
