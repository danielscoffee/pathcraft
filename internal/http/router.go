package http

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
)

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
	type NodeInfo struct {
		ID  graph.NodeID `json:"id"`
		Lat float64      `json:"lat"`
		Lon float64      `json:"lon"`
	}
	nodes := make([]NodeInfo, 0, len(s.Graph.Nodes))
	for _, n := range s.Graph.Nodes {
		nodes = append(nodes, NodeInfo{ID: n.ID, Lat: n.Lat, Lon: n.Lon})
	}

	resp := map[string]any{
		"nodes_count": len(s.Graph.Nodes),
		"edges_count": len(s.Graph.Edges),
		"nodes":       nodes,
	}

	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

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
		http.Error(w, fmt.Sprintf("route error: %v", err), http.StatusNotFound)
		return
	}

	err = json.NewEncoder(w).Encode(path)
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func RunServer(g *graph.Graph, addr string) {
	s := NewServer(g)
	s.SetupRoutes()
	log.Printf("Server running on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
