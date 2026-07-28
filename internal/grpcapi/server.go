package grpcapi

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	pathcraftv1 "github.com/danielscoffee/pathcraft/api/pathcraft/v1"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

const (
	maxRequestBytes = 1 << 20
	maxStopCount    = 64
	bikeSpeedMPS    = 4.5
)

type routingServer struct {
	pathcraftv1.UnimplementedRoutingServiceServer
	engine *engine.Engine
}

func NewServer(router *engine.Engine) *grpc.Server {
	server := grpc.NewServer(grpc.MaxRecvMsgSize(maxRequestBytes))
	pathcraftv1.RegisterRoutingServiceServer(server, &routingServer{engine: router})

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	healthStatus := grpc_health_v1.HealthCheckResponse_NOT_SERVING
	if router != nil && router.Stats().Nodes > 0 {
		healthStatus = grpc_health_v1.HealthCheckResponse_SERVING
	}
	healthServer.SetServingStatus("", healthStatus)
	healthServer.SetServingStatus(pathcraftv1.RoutingService_ServiceDesc.ServiceName, healthStatus)
	return server
}

func (server *routingServer) Route(ctx context.Context, request *pathcraftv1.RouteRequest) (*pathcraftv1.RouteResponse, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "route request is required")
	}
	if err := validateCoordinate("origin", request.Origin); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := validateCoordinate("destination", request.Destination); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	profile, err := profileForMode(request.Mode)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if server.engine == nil || server.engine.Stats().Nodes == 0 {
		return nil, status.Error(codes.FailedPrecondition, "street graph not loaded")
	}

	result, err := server.engine.RouteByCoordinates(engine.CoordinateRouteRequest{
		FromLat:            request.Origin.Latitude,
		FromLon:            request.Origin.Longitude,
		ToLat:              request.Destination.Latitude,
		ToLon:              request.Destination.Longitude,
		Profile:            profile,
		IncludeCoordinates: request.IncludeCoordinates,
	})
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return routeResponse(result), nil
}

func (server *routingServer) Journey(ctx context.Context, request *pathcraftv1.JourneyRequest) (*pathcraftv1.JourneyResponse, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "journey request is required")
	}
	if err := validateCoordinate("origin", request.Origin); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := validateCoordinate("destination", request.Destination); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if _, err := pcTime.ParseTime(request.DepartureTime); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid departure time: %v", err)
	}
	if request.MaxStopCount < 0 || request.MaxStopCount > maxStopCount {
		return nil, status.Errorf(codes.InvalidArgument, "max_stop_count must be between 0 and %d", maxStopCount)
	}
	if server.engine == nil || server.engine.Stats().Nodes == 0 {
		return nil, status.Error(codes.FailedPrecondition, "street graph not loaded")
	}

	result, err := server.engine.MultimodalRoute(engine.MultimodalRouteRequest{
		FromLat:       request.Origin.Latitude,
		FromLon:       request.Origin.Longitude,
		ToLat:         request.Destination.Latitude,
		ToLon:         request.Destination.Longitude,
		DepartureTime: request.DepartureTime,
		MaxStopCount:  int(request.MaxStopCount),
	})
	if err != nil {
		if strings.Contains(err.Error(), "GTFS not loaded") || strings.Contains(err.Error(), "GTFS stops with coordinates") {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.NotFound, err.Error())
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return journeyResponse(result), nil
}

func validateCoordinate(name string, coordinate *pathcraftv1.Coordinate) error {
	if coordinate == nil {
		return fmt.Errorf("%s coordinate is required", name)
	}
	if math.IsNaN(coordinate.Latitude) || math.IsInf(coordinate.Latitude, 0) || coordinate.Latitude < -90 || coordinate.Latitude > 90 {
		return fmt.Errorf("%s latitude must be finite and between -90 and 90", name)
	}
	if math.IsNaN(coordinate.Longitude) || math.IsInf(coordinate.Longitude, 0) || coordinate.Longitude < -180 || coordinate.Longitude > 180 {
		return fmt.Errorf("%s longitude must be finite and between -180 and 180", name)
	}
	return nil
}

func profileForMode(mode pathcraftv1.TravelMode) (mobility.Profile, error) {
	switch mode {
	case pathcraftv1.TravelMode_TRAVEL_MODE_UNSPECIFIED, pathcraftv1.TravelMode_TRAVEL_MODE_WALK:
		return mobility.NewWalking(mobility.DefaultWalkingSpeedMPS), nil
	case pathcraftv1.TravelMode_TRAVEL_MODE_BIKE:
		return mobility.NewWalking(bikeSpeedMPS), nil
	case pathcraftv1.TravelMode_TRAVEL_MODE_CAR:
		return mobility.NewDriving(mobility.DefaultDrivingSpeedMPS), nil
	default:
		return nil, fmt.Errorf("unsupported travel mode %d", mode)
	}
}

func routeResponse(result *engine.CoordinateRouteResult) *pathcraftv1.RouteResponse {
	return &pathcraftv1.RouteResponse{
		NodeIds:                       result.Nodes,
		Coordinates:                   coordinates(result.Coordinates),
		DistanceMeters:                result.Distance,
		DurationSeconds:               durationSeconds(result.Duration),
		OriginNodeId:                  result.FromNodeID,
		DestinationNodeId:             result.ToNodeID,
		OriginSnapDistanceMeters:      result.FromSnapDistanceM,
		DestinationSnapDistanceMeters: result.ToSnapDistanceM,
	}
}

func journeyResponse(result *engine.MultimodalRouteResult) *pathcraftv1.JourneyResponse {
	legs := make([]*pathcraftv1.JourneyLeg, len(result.Legs))
	for i, leg := range result.Legs {
		legs[i] = &pathcraftv1.JourneyLeg{
			Mode:            leg.Mode,
			FromName:        leg.FromName,
			ToName:          leg.ToName,
			FromStopId:      leg.FromStopID,
			ToStopId:        leg.ToStopID,
			TripId:          leg.TripID,
			RouteId:         leg.RouteID,
			RouteName:       leg.RouteName,
			RouteLongName:   leg.RouteLongName,
			DepartureTime:   leg.DepartureTime,
			ArrivalTime:     leg.ArrivalTime,
			DistanceMeters:  leg.DistanceM,
			DurationSeconds: durationSeconds(leg.Duration),
			NodeIds:         leg.Nodes,
			Coordinates:     coordinates(leg.Coordinates),
		}
	}
	return &pathcraftv1.JourneyResponse{
		Mode:                   result.Mode,
		DepartureTime:          result.DepartureTime,
		ArrivalTime:            result.ArrivalTime,
		TotalDurationSeconds:   durationSeconds(result.TotalDuration),
		TransitDurationSeconds: durationSeconds(result.TransitDuration),
		WalkingDistanceMeters:  result.WalkingDistanceM,
		Legs:                   legs,
		OriginStopId:           result.OriginStopID,
		DestinationStopId:      result.DestinationStopID,
	}
}

func coordinates(input []engine.Coordinate) []*pathcraftv1.Coordinate {
	output := make([]*pathcraftv1.Coordinate, len(input))
	for i, coordinate := range input {
		output[i] = &pathcraftv1.Coordinate{Latitude: coordinate.Lat, Longitude: coordinate.Lon}
	}
	return output
}

func durationSeconds(duration time.Duration) int64 {
	return int64(duration / time.Second)
}

func contextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	return nil
}
