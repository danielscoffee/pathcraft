package http

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/danielscoffee/pathcraft/internal/geojson"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

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

type journeyLegResponse struct {
	Mode            string              `json:"mode"`
	FromName        string              `json:"from_name,omitempty"`
	ToName          string              `json:"to_name,omitempty"`
	FromStopID      string              `json:"from_stop_id,omitempty"`
	ToStopID        string              `json:"to_stop_id,omitempty"`
	TripID          string              `json:"trip_id,omitempty"`
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

func NewServer(e *engine.Engine) *Server {
	return &Server{engine: e}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/journey", s.handleJourney)
	mux.HandleFunc("/route", s.handleRoute)
	mux.HandleFunc("/transit/stops", s.handleTransitStops)
	mux.HandleFunc("/transit/trips", s.handleTransitTrips)
	mux.HandleFunc("/transit/trip", s.handleTransitTrip)
	mux.HandleFunc("/nearest", s.handleNearest)
	mux.HandleFunc("/graph", s.handleGraph)
	mux.HandleFunc("/nodes", s.handleNodes)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/graph-visual", s.handleGraphVisual)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.handleStatus(w, r)
}

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

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id": %d, "distance": %f}`, id, dist)
}

func (s *Server) handleGraphVisual(w http.ResponseWriter, r *http.Request) {
	tpl, err := template.ParseFiles("./web/template/map.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	g := s.engine.GetGraph()
	centerLat, centerLon, zoom := -8.0540, -34.8800, 16
	if g != nil && len(g.Nodes) > 0 {
		var sumLat, sumLon float64
		for _, n := range g.Nodes {
			sumLat += n.Lat
			sumLon += n.Lon
		}
		centerLat = sumLat / float64(len(g.Nodes))
		centerLon = sumLon / float64(len(g.Nodes))
	}

	page := PageData{
		CenterLat: centerLat,
		CenterLon: centerLon,

		Zoom: zoom,

		TileURL: "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png",

		StreetsURL: "/graph",
		RouteURL:   "/route",

		StreetsColor:  "#555",
		StreetsWeight: 1,
		RouteColor:    "#e63946",
		RouteWeight:   4,
	}

	if err := tpl.Execute(w, page); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte(`{"status":"health"}`))
	if err != nil {
		http.Error(w, `{"status": "not health"}`, http.StatusInternalServerError)
		return
	}
}

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
		parts := splitFour(v)
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
	sortNodeIDs(ids)
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

func splitFour(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func sortNodeIDs(ids []graph.NodeID) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j-1] > ids[j]; j-- {
			ids[j-1], ids[j] = ids[j], ids[j-1]
		}
	}
}

func (s *Server) handleTransitStops(w http.ResponseWriter, r *http.Request) {
	stops := s.engine.GTFSStops()
	if len(stops) == 0 {
		http.Error(w, "GTFS stops not loaded", http.StatusServiceUnavailable)
		return
	}

	type geometry struct {
		Type        string     `json:"type"`
		Coordinates [2]float64 `json:"coordinates"`
	}
	type feature struct {
		Type       string            `json:"type"`
		Geometry   geometry          `json:"geometry"`
		Properties map[string]string `json:"properties"`
	}
	type featureCollection struct {
		Type     string    `json:"type"`
		Features []feature `json:"features"`
	}

	out := featureCollection{
		Type:     "FeatureCollection",
		Features: make([]feature, 0, len(stops)),
	}
	for _, stop := range stops {
		out.Features = append(out.Features, feature{
			Type: "Feature",
			Geometry: geometry{
				Type:        "Point",
				Coordinates: [2]float64{stop.Lon, stop.Lat},
			},
			Properties: map[string]string{
				"id":   stop.ID,
				"name": stop.Name,
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleTransitTrips(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string][]string{
		"trip_ids": s.engine.GTFSTripIDs(),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleTransitTrip(w http.ResponseWriter, r *http.Request) {
	tripID := r.URL.Query().Get("trip_id")
	stopTimes, err := s.engine.GTFSTripStopTimes(tripID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	type tripResponse struct {
		TripID      string                    `json:"trip_id"`
		RouteID     string                    `json:"route_id"`
		StopTimes   []engine.GTFSTripStopTime `json:"stop_times"`
		PathGeoJSON map[string]interface{}    `json:"path_geojson"`
	}

	lineCoordinates := make([][2]float64, 0, len(stopTimes))
	for _, stopTime := range stopTimes {
		lineCoordinates = append(lineCoordinates, [2]float64{stopTime.Lon, stopTime.Lat})
	}

	resp := tripResponse{
		TripID:    stopTimes[0].TripID,
		RouteID:   stopTimes[0].RouteID,
		StopTimes: stopTimes,
		PathGeoJSON: map[string]interface{}{
			"type": "FeatureCollection",
			"features": []map[string]interface{}{
				{
					"type": "Feature",
					"geometry": map[string]interface{}{
						"type":        "LineString",
						"coordinates": lineCoordinates,
					},
					"properties": map[string]interface{}{
						"trip_id":  stopTimes[0].TripID,
						"route_id": stopTimes[0].RouteID,
					},
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleRoute(w http.ResponseWriter, r *http.Request) {
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
			Profile: mobility.NewWalking(1.4),
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
		Profile: mobility.NewWalking(1.4),
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

func (s *Server) handleJourney(w http.ResponseWriter, r *http.Request) {
	fromLatStr := r.URL.Query().Get("from_lat")
	fromLonStr := r.URL.Query().Get("from_lon")
	toLatStr := r.URL.Query().Get("to_lat")
	toLonStr := r.URL.Query().Get("to_lon")
	depTime := r.URL.Query().Get("time")
	if fromLatStr == "" || fromLonStr == "" || toLatStr == "" || toLonStr == "" || depTime == "" {
		http.Error(w, "from_lat, from_lon, to_lat, to_lon, and time parameters required", http.StatusBadRequest)
		return
	}

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

	res, err := s.engine.MultimodalRoute(engine.MultimodalRouteRequest{
		FromLat:        fromLat,
		FromLon:        fromLon,
		ToLat:          toLat,
		ToLon:          toLon,
		DepartureTime:  depTime,
		WalkingProfile: mobility.NewWalking(1.4),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(toJourneyResponse(res)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func toJourneyResponse(res *engine.MultimodalRouteResult) journeyResponse {
	out := journeyResponse{
		Mode:                   res.Mode,
		DepartureTime:          res.DepartureTime,
		ArrivalTime:            res.ArrivalTime,
		TotalDurationSeconds:   int64(res.TotalDuration / time.Second),
		TransitDurationSeconds: int64(res.TransitDuration / time.Second),
		WalkingDistanceM:       res.WalkingDistanceM,
		OriginStopID:           res.OriginStopID,
		DestinationStopID:      res.DestinationStopID,
		TransitPath:            make([]journeyLegResponse, 0, len(res.TransitPath)),
		Legs:                   make([]journeyLegResponse, 0, len(res.Legs)),
	}

	for _, leg := range res.TransitPath {
		out.TransitPath = append(out.TransitPath, toJourneyLegResponse(leg))
	}
	for _, leg := range res.Legs {
		out.Legs = append(out.Legs, toJourneyLegResponse(leg))
	}

	return out
}

func toJourneyLegResponse(leg engine.JourneyLeg) journeyLegResponse {
	return journeyLegResponse{
		Mode:            leg.Mode,
		FromName:        leg.FromName,
		ToName:          leg.ToName,
		FromStopID:      leg.FromStopID,
		ToStopID:        leg.ToStopID,
		TripID:          leg.TripID,
		DistanceM:       leg.DistanceM,
		DurationSeconds: int64(leg.Duration / time.Second),
		Nodes:           leg.Nodes,
		Coordinates:     leg.Coordinates,
	}
}

func RunServer(e *engine.Engine, addr string) {
	s := NewServer(e)
	log.Printf("Server running on %s", addr)
	log.Fatal(http.ListenAndServe(addr, s.Handler()))
}
