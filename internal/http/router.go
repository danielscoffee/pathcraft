package http

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"

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

func NewServer(e *engine.Engine) *Server {
	return &Server{engine: e}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/route", s.handleRoute)
	mux.HandleFunc("/nearest", s.handleNearest)
	mux.HandleFunc("/graph", s.handleGraph)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/graph-visual", s.handleGraphVisual)
	return mux
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

	page := PageData{
		// It's hardcoded for now, but ideally it should be calculated from the graph's bounds
		CenterLat: -8.0545,
		CenterLon: -34.8807,

		Zoom: 17,

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

// WARN: To works need to do fetch on client side with from and to parameters
func (s *Server) handleRoute(w http.ResponseWriter, r *http.Request) {
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

func RunServer(e *engine.Engine, addr string) {
	s := NewServer(e)
	log.Printf("Server running on %s", addr)
	log.Fatal(http.ListenAndServe(addr, s.Handler()))
}
