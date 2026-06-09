package http

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/danielscoffee/pathcraft/internal/geojson"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
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
	profile := routeProfileFromMode(r.URL.Query().Get("mode"))
	fromLatStr := r.URL.Query().Get("from_lat")
	fromLonStr := r.URL.Query().Get("from_lon")
	toLatStr := r.URL.Query().Get("to_lat")
	toLonStr := r.URL.Query().Get("to_lon")
	if fromLatStr != "" || fromLonStr != "" || toLatStr != "" || toLonStr != "" {
		fromLat, err := strconv.ParseFloat(fromLatStr, 64)
		if err != nil {
			http.Error(w, "invalid from_lat parameter", http.StatusBadRequest)
			return
		}
		fromLon, err := strconv.ParseFloat(fromLonStr, 64)
		if err != nil {
			http.Error(w, "invalid from_lon parameter", http.StatusBadRequest)
			return
		}
		toLat, err := strconv.ParseFloat(toLatStr, 64)
		if err != nil {
			http.Error(w, "invalid to_lat parameter", http.StatusBadRequest)
			return
		}
		toLon, err := strconv.ParseFloat(toLonStr, 64)
		if err != nil {
			http.Error(w, "invalid to_lon parameter", http.StatusBadRequest)
			return
		}

		b, err := s.engine.RouteGeoJSONByCoordinates(engine.CoordinateRouteRequest{
			FromLat: fromLat,
			FromLon: fromLon,
			ToLat:   toLat,
			ToLon:   toLon,
			Profile: profile,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
		return
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	if fromStr == "" || toStr == "" {
		http.Error(w, "from and to parameters required", http.StatusBadRequest)
		return
	}

	fromID, err := strconv.ParseInt(fromStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid from parameter", http.StatusBadRequest)
		return
	}

	toID, err := strconv.ParseInt(toStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid to parameter", http.StatusBadRequest)
		return
	}

	res, err := s.engine.Route(engine.RouteRequest{
		From:    fromID,
		To:      toID,
		Profile: profile,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	ids := make([]graph.NodeID, len(res.Nodes))
	for i, n := range res.Nodes {
		ids[i] = graph.NodeID(n)
	}

	g := s.engine.GetGraph()
	if g == nil {
		http.Error(w, "graph not loaded", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	b := geojson.PathToGeoJSON(g, ids)

	w.Write(b)
}

func routeProfileFromMode(mode string) mobility.Profile {
	switch mode {
	case "car":
		return mobility.NewDriving(mobility.DefaultDrivingSpeedMPS)
	case "bike":
		return mobility.NewWalking(4.5)
	default:
		return mobility.NewWalking(1.4)
	}
}
