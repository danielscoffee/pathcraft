package grpcapi

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	pathcraftv1 "github.com/danielscoffee/pathcraft/api/pathcraft/v1"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

const (
	maxRequestBytes = 1 << 20
	maxStopCount    = 64
)

type routingServer struct {
	pathcraftv1.UnimplementedRoutingServiceServer
	engine   *engine.Engine
	registry *plugins.Registry
}

func NewServer(router *engine.Engine) *grpc.Server {
	return NewServerWithRegistry(router, plugins.Default)
}

func NewServerWithRegistry(router *engine.Engine, registry *plugins.Registry) *grpc.Server {
	if registry == nil {
		registry = plugins.Default
	}
	server := grpc.NewServer(grpc.MaxRecvMsgSize(maxRequestBytes))
	pathcraftv1.RegisterRoutingServiceServer(server, &routingServer{engine: router, registry: registry})

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
	if err := validateEndpoints(request.Origin, request.Destination); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	modeName, err := modeNameForTravelMode(request.Mode)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if server.engine == nil || server.engine.Stats().Nodes == 0 {
		return nil, status.Error(codes.FailedPrecondition, "street graph not loaded")
	}

	mode, ok := server.registry.Mode(modeName)
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "mode %q not registered", modeName)
	}
	result, err := mode.Route(ctx, server.engine, core.ModeRequest{
		From: core.Position{request.Origin.Longitude, request.Origin.Latitude},
		To:   core.Position{request.Destination.Longitude, request.Destination.Latitude},
	})
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return modeRouteResponse(result, request.IncludeCoordinates), nil
}

func (server *routingServer) Journey(ctx context.Context, request *pathcraftv1.JourneyRequest) (*pathcraftv1.JourneyResponse, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "journey request is required")
	}
	if err := validateEndpoints(request.Origin, request.Destination); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	departure, err := pcTime.ParseTime(request.DepartureTime)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid departure time: %v", err)
	}
	if request.MaxStopCount < 0 || request.MaxStopCount > maxStopCount {
		return nil, status.Errorf(codes.InvalidArgument, "max_stop_count must be between 0 and %d", maxStopCount)
	}
	if server.engine == nil || server.engine.Stats().Nodes == 0 {
		return nil, status.Error(codes.FailedPrecondition, "street graph not loaded")
	}

	mode, ok := server.registry.Mode("gtfs")
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "mode \"gtfs\" not registered")
	}
	result, err := mode.Route(ctx, server.engine, core.ModeRequest{
		From: core.Position{request.Origin.Longitude, request.Origin.Latitude},
		To:   core.Position{request.Destination.Longitude, request.Destination.Latitude},
		Options: map[string]string{
			"departure_time": departure.String(),
			"max_stop_count": strconv.FormatInt(int64(request.MaxStopCount), 10),
		},
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
	return modeJourneyResponse(result), nil
}

func validateEndpoints(origin, destination *pathcraftv1.Coordinate) error {
	if err := validateCoordinate("origin", origin); err != nil {
		return err
	}
	return validateCoordinate("destination", destination)
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

func modeNameForTravelMode(mode pathcraftv1.TravelMode) (string, error) {
	switch mode {
	case pathcraftv1.TravelMode_TRAVEL_MODE_UNSPECIFIED, pathcraftv1.TravelMode_TRAVEL_MODE_WALK:
		return "walk", nil
	case pathcraftv1.TravelMode_TRAVEL_MODE_BIKE:
		return "bike", nil
	case pathcraftv1.TravelMode_TRAVEL_MODE_CAR:
		return "car", nil
	default:
		return "", fmt.Errorf("unsupported travel mode %d", mode)
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

func modeRouteResponse(result core.ModeResult, includeCoordinates bool) *pathcraftv1.RouteResponse {
	nodes, _ := result.Meta["nodes"].([]int64)
	originNodeID, _ := result.Meta["from_node_id"].(int64)
	destinationNodeID, _ := result.Meta["to_node_id"].(int64)
	originSnapDistance, _ := result.Meta["from_snap_distance_m"].(float64)
	destinationSnapDistance, _ := result.Meta["to_snap_distance_m"].(float64)
	var routeCoordinates []*pathcraftv1.Coordinate
	if includeCoordinates {
		for _, segment := range result.Segments {
			for _, position := range segment.Positions {
				if len(position) >= 2 {
					routeCoordinates = append(routeCoordinates, &pathcraftv1.Coordinate{
						Longitude: position[0],
						Latitude:  position[1],
					})
				}
			}
		}
	}
	return &pathcraftv1.RouteResponse{
		NodeIds:                       nodes,
		Coordinates:                   routeCoordinates,
		DistanceMeters:                result.DistanceMeters,
		DurationSeconds:               result.DurationSeconds,
		OriginNodeId:                  originNodeID,
		DestinationNodeId:             destinationNodeID,
		OriginSnapDistanceMeters:      originSnapDistance,
		DestinationSnapDistanceMeters: destinationSnapDistance,
	}
}

func modeJourneyResponse(result core.ModeResult) *pathcraftv1.JourneyResponse {
	legs := make([]*pathcraftv1.JourneyLeg, 0, len(result.Segments))
	var transitDuration int64
	for _, segment := range result.Segments {
		coordinates := make([]*pathcraftv1.Coordinate, 0, len(segment.Positions))
		for _, position := range segment.Positions {
			if len(position) >= 2 {
				coordinates = append(coordinates, &pathcraftv1.Coordinate{Longitude: position[0], Latitude: position[1]})
			}
		}
		legs = append(legs, &pathcraftv1.JourneyLeg{
			Mode:            segment.Kind,
			FromName:        modeMetaString(segment.Meta, "from"),
			ToName:          modeMetaString(segment.Meta, "to"),
			FromStopId:      modeMetaString(segment.Meta, "from_stop_id"),
			ToStopId:        modeMetaString(segment.Meta, "to_stop_id"),
			TripId:          modeMetaString(segment.Meta, "trip_id"),
			RouteId:         modeMetaString(segment.Meta, "route_id"),
			RouteName:       modeMetaString(segment.Meta, "route_name"),
			RouteLongName:   modeMetaString(segment.Meta, "route_long_name"),
			DepartureTime:   modeMetaString(segment.Meta, "departure_time"),
			ArrivalTime:     modeMetaString(segment.Meta, "arrival_time"),
			DistanceMeters:  segment.DistanceMeters,
			DurationSeconds: segment.DurationSeconds,
			Coordinates:     coordinates,
		})
		if segment.Kind == "transit" || segment.Kind == "transfer" {
			transitDuration += segment.DurationSeconds
		}
	}
	mode := modeMetaString(result.Meta, "journey_mode")
	if mode == "" {
		mode = result.Mode
	}
	return &pathcraftv1.JourneyResponse{
		Mode:                   mode,
		DepartureTime:          modeMetaString(result.Meta, "departure_time"),
		ArrivalTime:            modeMetaString(result.Meta, "arrival_time"),
		TotalDurationSeconds:   result.DurationSeconds,
		TransitDurationSeconds: transitDuration,
		WalkingDistanceMeters:  result.DistanceMeters,
		Legs:                   legs,
		OriginStopId:           modeMetaString(result.Meta, "origin_stop_id"),
		DestinationStopId:      modeMetaString(result.Meta, "destination_stop_id"),
	}
}

func modeMetaString(meta map[string]any, key string) string {
	value, _ := meta[key].(string)
	return value
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
