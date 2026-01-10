package geojson

import (
	"encoding/json"
	"io"

	"github.com/danielscoffee/pathcraft/internal/graph"
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

func WriteGraphToGeoJSON(g *graph.Graph, w io.Writer) error {
	if _, err := w.Write([]byte(`{"type":"FeatureCollection","features":[`)); err != nil {
		return err
	}

	first := true
	for from, edges := range g.Edges {
		fromNode := g.Nodes[from]
		for _, e := range edges {
			if !first {
				if _, err := w.Write([]byte(`,`)); err != nil {
					return err
				}
			}
			first = false

			toNode := g.Nodes[e.To]
			feature := Feature{
				Type: "Feature",
				Geometry: map[string]any{
					"type":        "LineString",
					"coordinates": [][]float64{{fromNode.Lon, fromNode.Lat}, {toNode.Lon, toNode.Lat}},
				},
			}
			b, err := json.Marshal(feature)
			if err != nil {
				return err
			}
			if _, err := w.Write(b); err != nil {
				return err
			}
		}
	}

	_, err := w.Write([]byte(`]}`))
	return err
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
