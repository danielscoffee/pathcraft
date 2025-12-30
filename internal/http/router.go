package http

import (
	"html/template"
	"log"
	"net/http"
	"strconv"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/geojson"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
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
// TODO: TEST FILE

type Server struct {
	Graph *graph.Graph
}

func NewServer(g *graph.Graph) *Server {
	return &Server{Graph: g}
}

func (s *Server) SetupRoutes() {
	http.HandleFunc("/route", s.handleRoute)
	http.HandleFunc("/graph", s.handleGraph)
	http.HandleFunc("/status", s.handleStatus)
	http.HandleFunc("/graph-visual", s.handleGraphVisual)
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
	w.Header().Set("Content-Type", "application/json")

	b := geojson.GraphToGeoJSON(s.Graph)

	w.Write(b)
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

	h := geo.HaversineHeuristic(1.4)

	path, err := astar.AStar(s.Graph, graph.NodeID(fromID), graph.NodeID(toID), h)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	// WARN: THIS IS INEFFICIENT, THE IDEAL IS TO RETURN DIRECTLY THE IDS FROM THE ASTAR
	ids := make([]graph.NodeID, 0, len(path.Nodes))
	for _, n := range path.Nodes {
		ids = append(ids, n)
	}

	w.Header().Set("Content-Type", "application/json")

	b := geojson.PathToGeoJSON(s.Graph, ids)

	w.Write(b)
}

func RunServer(g *graph.Graph, addr string) {
	s := NewServer(g)
	s.SetupRoutes()
	log.Printf("Server running on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
