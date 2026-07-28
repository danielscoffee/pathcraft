package grpcapi

import (
	"context"
	"net"
	"slices"
	"testing"
	"time"

	pathcraftv1 "github.com/danielscoffee/pathcraft/api/pathcraft/v1"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const testBufferSize = 1 << 20

func testClient(t *testing.T, router *engine.Engine) (pathcraftv1.RoutingServiceClient, grpc_health_v1.HealthClient) {
	t.Helper()
	listener := bufconn.Listen(testBufferSize)
	server := NewServer(router)
	go func() { _ = server.Serve(listener) }()

	connection, err := grpc.NewClient("passthrough:///pathcraft-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		_ = connection.Close()
		server.Stop()
		_ = listener.Close()
	})
	return pathcraftv1.NewRoutingServiceClient(connection), grpc_health_v1.NewHealthClient(connection)
}

func loadedEngine(t *testing.T) *engine.Engine {
	t.Helper()
	router := engine.New()
	if err := router.LoadOSM("../../testdata/example.osm"); err != nil {
		t.Fatalf("LoadOSM() error = %v", err)
	}
	return router
}

func routeRequest(mode pathcraftv1.TravelMode) *pathcraftv1.RouteRequest {
	return &pathcraftv1.RouteRequest{
		Origin:             &pathcraftv1.Coordinate{Latitude: -8.05428, Longitude: -34.88130},
		Destination:        &pathcraftv1.Coordinate{Latitude: -8.05520, Longitude: -34.87970},
		Mode:               mode,
		IncludeCoordinates: true,
	}
}

func TestRouteReturnsStreetPath(t *testing.T) {
	client, _ := testClient(t, loadedEngine(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	walk, err := client.Route(ctx, routeRequest(pathcraftv1.TravelMode_TRAVEL_MODE_WALK))
	if err != nil {
		t.Fatalf("Route(walk) error = %v", err)
	}
	car, err := client.Route(ctx, routeRequest(pathcraftv1.TravelMode_TRAVEL_MODE_CAR))
	if err != nil {
		t.Fatalf("Route(car) error = %v", err)
	}

	if !slices.Equal(walk.NodeIds, []int64{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("node IDs = %v, want fixture path", walk.NodeIds)
	}
	if len(walk.Coordinates) != 6 || walk.OriginNodeId != 1 || walk.DestinationNodeId != 6 {
		t.Fatalf("route response = %+v", walk)
	}
	if walk.DistanceMeters <= 0 || walk.DurationSeconds <= car.DurationSeconds || car.DurationSeconds <= 0 {
		t.Fatalf("walk/car durations = %d/%d, distance = %f", walk.DurationSeconds, car.DurationSeconds, walk.DistanceMeters)
	}
}

func TestRouteValidatesRequest(t *testing.T) {
	client, _ := testClient(t, loadedEngine(t))
	tests := map[string]struct {
		request *pathcraftv1.RouteRequest
		code    codes.Code
	}{
		"missing origin": {
			request: &pathcraftv1.RouteRequest{Destination: &pathcraftv1.Coordinate{}},
			code:    codes.InvalidArgument,
		},
		"latitude range": {
			request: &pathcraftv1.RouteRequest{Origin: &pathcraftv1.Coordinate{Latitude: 91}, Destination: &pathcraftv1.Coordinate{}},
			code:    codes.InvalidArgument,
		},
		"unknown mode": {
			request: &pathcraftv1.RouteRequest{Origin: &pathcraftv1.Coordinate{}, Destination: &pathcraftv1.Coordinate{}, Mode: pathcraftv1.TravelMode(99)},
			code:    codes.InvalidArgument,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := client.Route(context.Background(), test.request)
			if status.Code(err) != test.code {
				t.Fatalf("Route() code = %v, want %v; error = %v", status.Code(err), test.code, err)
			}
		})
	}
}

func TestRouteRequiresLoadedGraph(t *testing.T) {
	client, _ := testClient(t, engine.New())
	_, err := client.Route(context.Background(), routeRequest(pathcraftv1.TravelMode_TRAVEL_MODE_WALK))
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Route() code = %v, want FailedPrecondition; error = %v", status.Code(err), err)
	}
}

func TestJourneyValidatesDataAndDeparture(t *testing.T) {
	client, _ := testClient(t, loadedEngine(t))
	request := &pathcraftv1.JourneyRequest{
		Origin:      &pathcraftv1.Coordinate{Latitude: -8.05428, Longitude: -34.88130},
		Destination: &pathcraftv1.Coordinate{Latitude: -8.05520, Longitude: -34.87970},
	}

	_, err := client.Journey(context.Background(), request)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Journey(empty departure) code = %v, want InvalidArgument", status.Code(err))
	}
	request.DepartureTime = "08:00:00"
	_, err = client.Journey(context.Background(), request)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Journey(without GTFS) code = %v, want FailedPrecondition; error = %v", status.Code(err), err)
	}
	request.MaxStopCount = 65
	_, err = client.Journey(context.Background(), request)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Journey(max stop count) code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestHealthReportsServing(t *testing.T) {
	_, healthClient := testClient(t, loadedEngine(t))
	response, err := healthClient.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{
		Service: pathcraftv1.RoutingService_ServiceDesc.ServiceName,
	})
	if err != nil {
		t.Fatalf("Health.Check() error = %v", err)
	}
	if response.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("health status = %v, want SERVING", response.Status)
	}
}

func TestHealthReportsNotServingWithoutGraph(t *testing.T) {
	_, healthClient := testClient(t, engine.New())
	response, err := healthClient.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{
		Service: pathcraftv1.RoutingService_ServiceDesc.ServiceName,
	})
	if err != nil {
		t.Fatalf("Health.Check() error = %v", err)
	}
	if response.Status != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("health status = %v, want NOT_SERVING", response.Status)
	}
}

func TestJourneyResponseMapsLegs(t *testing.T) {
	response := journeyResponse(&engine.MultimodalRouteResult{
		Mode:              "multimodal",
		DepartureTime:     "08:00:00",
		ArrivalTime:       "08:10:00",
		TotalDuration:     10 * time.Minute,
		TransitDuration:   8 * time.Minute,
		WalkingDistanceM:  120,
		OriginStopID:      "A",
		DestinationStopID: "B",
		Legs: []engine.JourneyLeg{{
			Mode:          "transit",
			FromStopID:    "A",
			ToStopID:      "B",
			TripID:        "trip",
			DepartureTime: "08:01:00",
			ArrivalTime:   "08:09:00",
			Duration:      8 * time.Minute,
			Nodes:         []int64{1, 2},
			Coordinates:   []engine.Coordinate{{Lat: -8, Lon: -34}},
		}},
	})

	if response.TotalDurationSeconds != 600 || len(response.Legs) != 1 {
		t.Fatalf("journey response = %+v", response)
	}
	leg := response.Legs[0]
	if leg.TripId != "trip" || leg.DurationSeconds != 480 || !slices.Equal(leg.NodeIds, []int64{1, 2}) || len(leg.Coordinates) != 1 {
		t.Fatalf("journey leg = %+v", leg)
	}
}
