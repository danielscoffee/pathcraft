package http

import (
	"encoding/json"
	"net/http"

	"github.com/danielscoffee/pathcraft/internal/logging"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"go.uber.org/zap"
)

func NewServer(e *engine.Engine) *Server {
	return &Server{engine: e}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/journey", s.handleJourney)
	mux.HandleFunc("/modes", s.handleModes)
	mux.HandleFunc("/route", s.handleRoute)
	mux.HandleFunc("/transit/stops", s.handleTransitStops)
	mux.HandleFunc("/transit/trips", s.handleTransitTrips)
	mux.HandleFunc("/transit/trip", s.handleTransitTrip)
	mux.HandleFunc("/nearest", s.handleNearest)
	mux.HandleFunc("/graph", s.handleGraph)
	mux.HandleFunc("/nodes", s.handleNodes)
	mux.HandleFunc("/config", s.handleConfig)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	// Legacy demo URL; the SPA now lives at the root.
	mux.Handle("/graph-visual", http.RedirectHandler("/", http.StatusMovedPermanently))
	mux.Handle("/", spaHandler())
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.handleStatus(w, r)
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

func (s *Server) handleModes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(modeResponse{Modes: []routeMode{
		{ID: "walk", Label: "Walk", Kind: "standard", Endpoint: "/route"},
		{ID: "bus", Label: "Bus / GTFS", Kind: "gtfs", Endpoint: "/journey"},
		{ID: "car", Label: "Car", Kind: "standard", Endpoint: "/route"},
		{ID: "bike", Label: "Bike", Kind: "standard", Endpoint: "/route"},
	}}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func RunServer(e *engine.Engine, addr string) {
	s := NewServer(e)
	logging.L().Info("server starting", zap.String("addr", addr))
	if err := http.ListenAndServe(addr, s.Handler()); err != nil {
		logging.L().Fatal("server stopped", zap.String("addr", addr), zap.Error(err))
	}
}
