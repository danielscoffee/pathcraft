package http

import "github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"

type PageData struct {
	// View
	CenterLat float64
	CenterLon float64
	Zoom      int

	// Tiles
	TileURL string

	// Data endpoints
	StreetsURL string
	RouteURL   string

	// Styles
	StreetsColor  string
	StreetsWeight int
	RouteColor    string
	RouteWeight   int
}

// WARN: THIS ROUTER IS MORE TO DEBUG AND TEST THE GEOJSON OUTPUTS AND BASIC ROUTING THAN A PRODUCTION FEAT

type Server struct {
	engine *engine.Engine
}

type modeResponse struct {
	Modes []routeMode `json:"modes"`
}

type routeMode struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	Endpoint string `json:"endpoint"`
}

type journeyLegResponse struct {
	Mode            string              `json:"mode"`
	FromName        string              `json:"from_name,omitempty"`
	ToName          string              `json:"to_name,omitempty"`
	FromStopID      string              `json:"from_stop_id,omitempty"`
	ToStopID        string              `json:"to_stop_id,omitempty"`
	TripID          string              `json:"trip_id,omitempty"`
	RouteID         string              `json:"route_id,omitempty"`
	RouteName       string              `json:"route_name,omitempty"`
	RouteLongName   string              `json:"route_long_name,omitempty"`
	DistanceM       float64             `json:"distance_meters,omitempty"`
	DurationSeconds int64               `json:"duration_seconds"`
	Nodes           []int64             `json:"nodes,omitempty"`
	Coordinates     []engine.Coordinate `json:"coordinates,omitempty"`
}

type journeyResponse struct {
	Mode                   string               `json:"mode"`
	DepartureTime          string               `json:"departure_time"`
	ArrivalTime            string               `json:"arrival_time"`
	TotalDurationSeconds   int64                `json:"total_duration_seconds"`
	TransitDurationSeconds int64                `json:"transit_duration_seconds,omitempty"`
	WalkingDistanceM       float64              `json:"walking_distance_meters,omitempty"`
	OriginStopID           string               `json:"origin_stop_id,omitempty"`
	DestinationStopID      string               `json:"destination_stop_id,omitempty"`
	TransitPath            []journeyLegResponse `json:"transit_path,omitempty"`
	Legs                   []journeyLegResponse `json:"legs"`
}
