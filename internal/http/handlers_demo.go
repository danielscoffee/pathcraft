package http

import (
	"html/template"
	"net/http"
)

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
