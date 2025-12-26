package geojson

import (
	"encoding/json"

	"github.com/qoppatech/exp-pathcraft/internal/domain/graph"
)

type Feature struct {
	Type       string         `json:"type"`
	Geometry   map[string]any `json:"geometry"`
	Properties map[string]any `json:"properties,omitempty"`
}
type FeatureCollection struct {
	Type     string    `json:"type"`
	Features []Feature `json:"features"`
}

// TODO: IMPLEMENT DYNAMICS CONVERSION - STUDY HOW TO DO THIS
// PERFOMATICALLY

func GraphToGeoJSON(g *graph.Graph) []byte {
	var features []Feature
	for from, edges := range g.Edges {
		fromNode := g.Nodes[from]
		for _, e := range edges {
			toNode := g.Nodes[e.To]
			features = append(features, Feature{
				Type: "Feature",
				Geometry: map[string]any{
					"type":        "LineString",
					"coordinates": [][]float64{{fromNode.Lon, fromNode.Lat}, {toNode.Lon, toNode.Lat}},
				},
			})
		}
	}

	fc := FeatureCollection{
		Type:     "FeatureCollection",
		Features: features,
	}

	b, _ := json.Marshal(fc)
	return b
}

func PathToGeoJSON(g *graph.Graph, path []graph.NodeID) []byte {
	var coords [][]float64
	for _, id := range path {
		n := g.Nodes[id]
		coords = append(coords, []float64{n.Lon, n.Lat})
	}

	fc := FeatureCollection{
		Type: "FeatureCollection",
		Features: []Feature{
			{
				Type: "Feature",
				Geometry: map[string]any{
					"type":        "LineString",
					"coordinates": coords,
				},
				Properties: map[string]any{"route": true},
			},
		},
	}

	b, _ := json.Marshal(fc)
	return b
}
