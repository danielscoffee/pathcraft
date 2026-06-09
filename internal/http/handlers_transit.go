package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

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

func (s *Server) handleJourney(w http.ResponseWriter, r *http.Request) {
	fromLatStr := r.URL.Query().Get("from_lat")
	fromLonStr := r.URL.Query().Get("from_lon")
	toLatStr := r.URL.Query().Get("to_lat")
	toLonStr := r.URL.Query().Get("to_lon")
	depTime := normalizeGTFSClockTime(r.URL.Query().Get("time"))
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

func normalizeGTFSClockTime(value string) string {
	if len(value) == len("15:04") {
		return value + ":00"
	}
	return value
}
