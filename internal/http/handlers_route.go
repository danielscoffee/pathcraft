package http

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
)

func (s *Server) handleNearest(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")

	if latStr == "" || lonStr == "" {
		http.Error(w, "lat and lon parameters required", http.StatusBadRequest)
		return
	}

	lat, err := parseGeographicCoordinate("lat", latStr, -90, 90)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	lon, err := parseGeographicCoordinate("lon", lonStr, -180, 180)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if host, ok := s.modeHost.(nearestPositionHost); ok {
		id, snapLat, snapLon, dist, err := host.NearestPosition(r.Context(), lat, lon)
		if err != nil {
			writeModeHostError(w, s.modeHost, err, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id": %d, "distance": %f, "lat": %f, "lon": %f}`, id, dist, snapLat, snapLon)
		return
	}
	if s.engine == nil {
		http.Error(w, "graph not loaded", http.StatusServiceUnavailable)
		return
	}
	id, dist, err := s.engine.NearestNode(lat, lon)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	g := s.engine.GetGraph()
	if g == nil {
		http.Error(w, "graph not loaded", http.StatusServiceUnavailable)
		return
	}
	node, ok := g.Nodes[graph.NodeID(id)]
	if !ok {
		http.Error(w, "nearest node not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id": %d, "distance": %f, "lat": %f, "lon": %f}`, id, dist, node.Lat, node.Lon)
}

func parseGeographicCoordinate(name, value string, minValue, maxValue float64) (float64, error) {
	coordinate, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(coordinate) || math.IsInf(coordinate, 0) || coordinate < minValue || coordinate > maxValue {
		return 0, fmt.Errorf("invalid %s parameter", name)
	}
	return coordinate, nil
}

func (s *Server) handleRoute(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	modeName := query.Get("mode")
	if modeName == "" {
		modeName = "walk"
	}
	mode, ok := s.registry.Mode(modeName)
	if !ok {
		http.Error(w, fmt.Sprintf("mode %q not registered", modeName), http.StatusNotFound)
		return
	}

	from, to, err := s.legacyRoutePositions(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := mode.Route(r.Context(), s.modeHost, core.ModeRequest{From: from, To: to})
	if err != nil {
		writeModeHostError(w, s.modeHost, err, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(modeResultFeatureCollection(result)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) legacyRoutePositions(r *http.Request) (core.Position, core.Position, error) {
	query := r.URL.Query()
	fromLatStr := query.Get("from_lat")
	fromLonStr := query.Get("from_lon")
	toLatStr := query.Get("to_lat")
	toLonStr := query.Get("to_lon")
	if fromLatStr != "" || fromLonStr != "" || toLatStr != "" || toLonStr != "" {
		fromLat, err := parseGeographicCoordinate("from_lat", fromLatStr, -90, 90)
		if err != nil {
			return nil, nil, err
		}
		fromLon, err := parseGeographicCoordinate("from_lon", fromLonStr, -180, 180)
		if err != nil {
			return nil, nil, err
		}
		toLat, err := parseGeographicCoordinate("to_lat", toLatStr, -90, 90)
		if err != nil {
			return nil, nil, err
		}
		toLon, err := parseGeographicCoordinate("to_lon", toLonStr, -180, 180)
		if err != nil {
			return nil, nil, err
		}
		return core.Position{fromLon, fromLat}, core.Position{toLon, toLat}, nil
	}

	fromID, err := strconv.ParseInt(query.Get("from"), 10, 64)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid from parameter")
	}
	toID, err := strconv.ParseInt(query.Get("to"), 10, 64)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid to parameter")
	}
	if s.engine == nil {
		return nil, nil, fmt.Errorf("graph not loaded")
	}
	g := s.engine.GetGraph()
	if g == nil {
		return nil, nil, fmt.Errorf("graph not loaded")
	}
	fromNode, fromOK := g.Nodes[graph.NodeID(fromID)]
	toNode, toOK := g.Nodes[graph.NodeID(toID)]
	if !fromOK || !toOK {
		return nil, nil, fmt.Errorf("route node not found")
	}
	return core.Position{fromNode.Lon, fromNode.Lat}, core.Position{toNode.Lon, toNode.Lat}, nil
}

func modeResultFeatureCollection(result core.ModeResult) map[string]any {
	features := make([]map[string]any, 0, len(result.Segments))
	for _, segment := range result.Segments {
		if len(segment.Positions) < 2 {
			continue
		}
		properties := make(map[string]any, len(segment.Meta)+7)
		for key, value := range segment.Meta {
			properties[key] = value
		}
		properties["mode"] = segment.Kind
		properties["label"] = segment.Label
		properties["color"] = segment.Color
		properties["dashed"] = segment.Dashed
		properties["distance_meters"] = segment.DistanceMeters
		properties["duration_seconds"] = segment.DurationSeconds
		features = append(features, map[string]any{
			"type": "Feature",
			"geometry": map[string]any{
				"type":        "LineString",
				"coordinates": segment.Positions,
			},
			"properties": properties,
		})
	}
	return map[string]any{"type": "FeatureCollection", "features": features}
}
