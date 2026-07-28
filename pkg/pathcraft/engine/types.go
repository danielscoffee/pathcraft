package engine

import (
	"time"

	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
)

const defaultStopCandidates = 8

type Engine struct {
	config         Config
	graph          *graph.Graph
	gtfsIndex      *gtfs.StopTimeIndex
	gtfsStops      map[gtfs.StopID]gtfs.Stop
	gtfsTransfers  map[gtfs.StopID][]raptor.Transfer
	gtfsTripRoutes map[gtfs.TripID]gtfs.RouteID
	gtfsTripShapes map[gtfs.TripID]gtfs.ShapeID
	gtfsRoutes     map[gtfs.RouteID]gtfs.Route
	gtfsShapes     map[gtfs.ShapeID][]gtfs.ShapePoint
}

func New() *Engine {
	return &Engine{config: DefaultConfig()}
}

type RouteRequest struct {
	From               int64
	To                 int64
	Profile            mobility.Profile
	IncludeCoordinates bool
}

type Coordinate struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type RouteResult struct {
	Nodes       []int64       `json:"nodes"`
	Coordinates []Coordinate  `json:"coordinates,omitempty"`
	Distance    float64       `json:"distance_meters"`
	Duration    time.Duration `json:"duration"`
}

type CoordinateRouteRequest struct {
	FromLat             float64
	FromLon             float64
	ToLat               float64
	ToLon               float64
	Profile             mobility.Profile
	IncludeCoordinates  bool
	IncludeInputInShape bool
}

type CoordinateRouteResult struct {
	RouteResult
	FromNodeID        int64   `json:"from_node_id"`
	ToNodeID          int64   `json:"to_node_id"`
	FromSnapDistanceM float64 `json:"from_snap_distance_meters"`
	ToSnapDistanceM   float64 `json:"to_snap_distance_meters"`
}

type TransitRouteRequest struct {
	FromStop      string
	ToStop        string
	DepartureTime string // HH:MM:SS
}

type MultimodalRouteRequest struct {
	FromLat        float64
	FromLon        float64
	ToLat          float64
	ToLon          float64
	DepartureTime  string
	WalkingProfile mobility.Profile
	MaxStopCount   int
}

type JourneyLeg struct {
	Mode          string        `json:"mode"`
	FromName      string        `json:"from_name,omitempty"`
	ToName        string        `json:"to_name,omitempty"`
	FromStopID    string        `json:"from_stop_id,omitempty"`
	ToStopID      string        `json:"to_stop_id,omitempty"`
	TripID        string        `json:"trip_id,omitempty"`
	RouteID       string        `json:"route_id,omitempty"`
	RouteName     string        `json:"route_name,omitempty"`
	RouteLongName string        `json:"route_long_name,omitempty"`
	DepartureTime string        `json:"departure_time"`
	ArrivalTime   string        `json:"arrival_time"`
	DistanceM     float64       `json:"distance_meters,omitempty"`
	Duration      time.Duration `json:"duration"`
	Nodes         []int64       `json:"nodes,omitempty"`
	Coordinates   []Coordinate  `json:"coordinates,omitempty"`
}

type MultimodalRouteResult struct {
	Mode              string        `json:"mode"`
	DepartureTime     string        `json:"departure_time"`
	ArrivalTime       string        `json:"arrival_time"`
	TotalDuration     time.Duration `json:"total_duration"`
	TransitDuration   time.Duration `json:"transit_duration,omitempty"`
	WalkingDistanceM  float64       `json:"walking_distance_meters,omitempty"`
	TransitPath       []JourneyLeg  `json:"transit_path,omitempty"`
	Legs              []JourneyLeg  `json:"legs"`
	OriginStopID      string        `json:"origin_stop_id,omitempty"`
	DestinationStopID string        `json:"destination_stop_id,omitempty"`
}

type GraphStats struct {
	Nodes int
	Edges int
}

type GTFSStop struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

type GTFSTripStopTime struct {
	TripID        string  `json:"trip_id"`
	RouteID       string  `json:"route_id"`
	StopID        string  `json:"stop_id"`
	StopName      string  `json:"stop_name"`
	ArrivalTime   string  `json:"arrival_time"`
	DepartureTime string  `json:"departure_time"`
	StopSequence  int     `json:"stop_sequence"`
	Lat           float64 `json:"lat"`
	Lon           float64 `json:"lon"`
}

type stopCandidate struct {
	stop      gtfs.Stop
	distanceM float64
	nodeID    int64
	snapDistM float64
}
