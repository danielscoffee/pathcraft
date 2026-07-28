package http

import (
	"encoding/json"
	"fmt"
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

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		http.Error(w, "invalid lat parameter", http.StatusBadRequest)
		return
	}

	lon, err := strconv.ParseFloat(lonStr, 64)
	if err != nil {
		http.Error(w, "invalid lon parameter", http.StatusBadRequest)
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
	result, err := mode.Route(r.Context(), s.engine, core.ModeRequest{From: from, To: to})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
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
		fromLat, err := strconv.ParseFloat(fromLatStr, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid from_lat parameter")
		}
		fromLon, err := strconv.ParseFloat(fromLonStr, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid from_lon parameter")
		}
		toLat, err := strconv.ParseFloat(toLatStr, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid to_lat parameter")
		}
		toLon, err := strconv.ParseFloat(toLonStr, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid to_lon parameter")
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
