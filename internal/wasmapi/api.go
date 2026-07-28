package wasmapi

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

const bikeSpeedMPS = 4.5

type API struct {
	engine *engine.Engine
}

type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type routeRequest struct {
	From               *Point `json:"from"`
	To                 *Point `json:"to"`
	Mode               string `json:"mode"`
	IncludeCoordinates bool   `json:"includeCoordinates"`
}

type routeResponse struct {
	Nodes                  []int64 `json:"nodes"`
	Coordinates            []Point `json:"coordinates,omitempty"`
	DistanceMeters         float64 `json:"distanceMeters"`
	DurationSeconds        float64 `json:"durationSeconds"`
	FromNodeID             int64   `json:"fromNodeId"`
	ToNodeID               int64   `json:"toNodeId"`
	FromSnapDistanceMeters float64 `json:"fromSnapDistanceMeters"`
	ToSnapDistanceMeters   float64 `json:"toSnapDistanceMeters"`
}

type statsResponse struct {
	Nodes             int `json:"nodes"`
	Edges             int `json:"edges"`
	ContractedNodes   int `json:"contractedNodes"`
	ContractionChains int `json:"contractionChains"`
}

type envelope struct {
	OK    bool   `json:"ok"`
	Value any    `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

func New() *API {
	return &API{engine: engine.New()}
}

func (api *API) LoadOSM(xml string) string {
	if err := api.engine.LoadOSMReader(strings.NewReader(xml)); err != nil {
		return failure(err)
	}
	return success(api.stats())
}

func (api *API) Route(requestJSON string) string {
	var request routeRequest
	if err := decodeStrict(requestJSON, &request); err != nil {
		return failure(fmt.Errorf("invalid route request: %w", err))
	}
	if request.From == nil || request.To == nil {
		return failure(fmt.Errorf("from and to coordinates are required"))
	}
	if err := validatePoint("from", *request.From); err != nil {
		return failure(err)
	}
	if err := validatePoint("to", *request.To); err != nil {
		return failure(err)
	}
	profile, err := profileForMode(request.Mode)
	if err != nil {
		return failure(err)
	}

	result, err := api.engine.RouteByCoordinates(engine.CoordinateRouteRequest{
		FromLat:            request.From.Lat,
		FromLon:            request.From.Lon,
		ToLat:              request.To.Lat,
		ToLon:              request.To.Lon,
		Profile:            profile,
		IncludeCoordinates: request.IncludeCoordinates,
	})
	if err != nil {
		return failure(err)
	}

	coordinates := make([]Point, len(result.Coordinates))
	for i, coordinate := range result.Coordinates {
		coordinates[i] = Point{Lat: coordinate.Lat, Lon: coordinate.Lon}
	}
	return success(routeResponse{
		Nodes:                  result.Nodes,
		Coordinates:            coordinates,
		DistanceMeters:         result.Distance,
		DurationSeconds:        result.Duration.Seconds(),
		FromNodeID:             result.FromNodeID,
		ToNodeID:               result.ToNodeID,
		FromSnapDistanceMeters: result.FromSnapDistanceM,
		ToSnapDistanceMeters:   result.ToSnapDistanceM,
	})
}

func (api *API) Stats() string {
	return success(api.stats())
}

func (api *API) stats() statsResponse {
	stats := api.engine.Stats()
	return statsResponse{
		Nodes:             stats.Nodes,
		Edges:             stats.Edges,
		ContractedNodes:   stats.ContractedNodes,
		ContractionChains: stats.ContractionChains,
	}
}

func decodeStrict(raw string, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func validatePoint(name string, point Point) error {
	if math.IsNaN(point.Lat) || math.IsInf(point.Lat, 0) || point.Lat < -90 || point.Lat > 90 {
		return fmt.Errorf("%s latitude must be finite and between -90 and 90", name)
	}
	if math.IsNaN(point.Lon) || math.IsInf(point.Lon, 0) || point.Lon < -180 || point.Lon > 180 {
		return fmt.Errorf("%s longitude must be finite and between -180 and 180", name)
	}
	return nil
}

func profileForMode(mode string) (mobility.Profile, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "walk":
		return mobility.NewWalking(mobility.DefaultWalkingSpeedMPS), nil
	case "bike":
		return mobility.NewWalking(bikeSpeedMPS), nil
	case "car":
		return mobility.NewDriving(mobility.DefaultDrivingSpeedMPS), nil
	default:
		return nil, fmt.Errorf("unsupported mode %q", mode)
	}
}

func success(value any) string {
	return marshal(envelope{OK: true, Value: value})
}

func failure(err error) string {
	return marshal(envelope{Error: err.Error()})
}

func marshal(value envelope) string {
	data, err := json.Marshal(value)
	if err != nil {
		return `{"ok":false,"error":"failed to encode response"}`
	}
	return string(data)
}
