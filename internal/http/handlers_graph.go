package http

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/danielscoffee/pathcraft/internal/geojson"
	"github.com/danielscoffee/pathcraft/internal/graph"
)

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	g := s.engine.GetGraph()
	if g == nil {
		http.Error(w, "graph not loaded", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := geojson.WriteGraphToGeoJSON(g, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	g := s.engine.GetGraph()
	if g == nil {
		http.Error(w, "graph not loaded", http.StatusServiceUnavailable)
		return
	}

	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	minDegree := 0
	if v := r.URL.Query().Get("min_degree"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			minDegree = n
		}
	}

	var minLon, minLat, maxLon, maxLat float64
	hasBBox := false
	if v := r.URL.Query().Get("bbox"); v != "" {
		parts := strings.Split(v, ",")
		if len(parts) == 4 {
			a, e1 := strconv.ParseFloat(parts[0], 64)
			b, e2 := strconv.ParseFloat(parts[1], 64)
			c, e3 := strconv.ParseFloat(parts[2], 64)
			d, e4 := strconv.ParseFloat(parts[3], 64)
			if e1 == nil && e2 == nil && e3 == nil && e4 == nil {
				minLon, minLat, maxLon, maxLat = a, b, c, d
				hasBBox = true
			}
		}
	}

	type geometry struct {
		Type        string     `json:"type"`
		Coordinates [2]float64 `json:"coordinates"`
	}
	type feature struct {
		Type       string         `json:"type"`
		Geometry   geometry       `json:"geometry"`
		Properties map[string]any `json:"properties"`
	}
	type featureCollection struct {
		Type     string    `json:"type"`
		Features []feature `json:"features"`
	}

	ids := make([]graph.NodeID, 0, len(g.Nodes))
	for id, n := range g.Nodes {
		if hasBBox && (n.Lon < minLon || n.Lon > maxLon || n.Lat < minLat || n.Lat > maxLat) {
			continue
		}
		if minDegree > 0 && len(g.Neighbors(id)) < minDegree {
			continue
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if limit > 0 && limit < len(ids) {
		ids = ids[:limit]
	}

	out := featureCollection{Type: "FeatureCollection", Features: make([]feature, 0, len(ids))}
	for _, id := range ids {
		n := g.Nodes[id]
		out.Features = append(out.Features, feature{
			Type:     "Feature",
			Geometry: geometry{Type: "Point", Coordinates: [2]float64{n.Lon, n.Lat}},
			Properties: map[string]any{
				"id":     int64(id),
				"degree": len(g.Neighbors(id)),
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
