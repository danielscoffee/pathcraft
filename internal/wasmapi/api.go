package wasmapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

type API struct {
	engine   *engine.Engine
	registry *plugins.Registry
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
	return NewWithRegistry(plugins.Default)
}

func NewWithRegistry(registry *plugins.Registry) *API {
	if registry == nil {
		registry = plugins.Default
	}
	return &API{engine: engine.New(), registry: registry}
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
	modeName := strings.ToLower(strings.TrimSpace(request.Mode))
	if modeName == "" {
		modeName = "walk"
	}
	mode, ok := api.registry.Mode(modeName)
	if !ok {
		return failure(fmt.Errorf("unsupported mode %q", request.Mode))
	}
	result, err := mode.Route(context.Background(), api.engine, core.ModeRequest{
		From: core.Position{request.From.Lon, request.From.Lat},
		To:   core.Position{request.To.Lon, request.To.Lat},
	})
	if err != nil {
		return failure(err)
	}

	coordinates := make([]Point, 0)
	if request.IncludeCoordinates {
		for _, segment := range result.Segments {
			for _, position := range segment.Positions {
				if len(position) >= 2 {
					coordinates = append(coordinates, Point{Lat: position[1], Lon: position[0]})
				}
			}
		}
	}
	nodes, _ := result.Meta["nodes"].([]int64)
	fromNodeID, _ := result.Meta["from_node_id"].(int64)
	toNodeID, _ := result.Meta["to_node_id"].(int64)
	fromSnapDistance, _ := result.Meta["from_snap_distance_m"].(float64)
	toSnapDistance, _ := result.Meta["to_snap_distance_m"].(float64)
	return success(routeResponse{
		Nodes:                  nodes,
		Coordinates:            coordinates,
		DistanceMeters:         result.DistanceMeters,
		DurationSeconds:        float64(result.DurationSeconds),
		FromNodeID:             fromNodeID,
		ToNodeID:               toNodeID,
		FromSnapDistanceMeters: fromSnapDistance,
		ToSnapDistanceMeters:   toSnapDistance,
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
