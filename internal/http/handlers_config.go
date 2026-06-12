package http

import (
	"encoding/json"
	"net/http"
)

type configResponse struct {
	CenterLat float64 `json:"center_lat"`
	CenterLon float64 `json:"center_lon"`
	Zoom      int     `json:"zoom"`
	TileURL   string  `json:"tile_url"`
}

// handleConfig exposes the map bootstrap data (center, zoom, tile source)
// that used to be injected into the HTML template, so the SPA can fetch it.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	centerLat, centerLon, zoom := -8.0540, -34.8800, 16
	if g := s.engine.GetGraph(); g != nil && len(g.Nodes) > 0 {
		var sumLat, sumLon float64
		for _, n := range g.Nodes {
			sumLat += n.Lat
			sumLon += n.Lon
		}
		centerLat = sumLat / float64(len(g.Nodes))
		centerLon = sumLon / float64(len(g.Nodes))
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(configResponse{
		CenterLat: centerLat,
		CenterLon: centerLon,
		Zoom:      zoom,
		TileURL:   "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png",
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
