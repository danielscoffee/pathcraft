// Package geojson registers the "geojson" Exporter that serializes a
// RouteResult into a GeoJSON FeatureCollection. It requires the graph to
// expose coordinates via core.Coordinated.
package geojson

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

type Exporter struct{}

func (Exporter) Name() string     { return "geojson" }
func (Exporter) MimeType() string { return "application/geo+json" }

type feature struct {
	Type       string         `json:"type"`
	Geometry   map[string]any `json:"geometry"`
	Properties map[string]any `json:"properties,omitempty"`
}

type featureCollection struct {
	Type     string    `json:"type"`
	Features []feature `json:"features"`
}

func (Exporter) Export(_ context.Context, result core.RouteResult, g core.Graph) ([]byte, error) {
	coords := make([][]float64, 0, len(result.Path))
	if c, ok := g.(core.Coordinated); ok {
		for _, id := range result.Path {
			lat, lon, ok := c.Coord(id)
			if !ok {
				continue
			}
			coords = append(coords, []float64{lon, lat})
		}
	}
	if len(coords) == 0 {
		return nil, fmt.Errorf("geojson: graph does not expose coordinates for path nodes")
	}

	fc := featureCollection{
		Type: "FeatureCollection",
		Features: []feature{{
			Type: "Feature",
			Geometry: map[string]any{
				"type":        "LineString",
				"coordinates": coords,
			},
			Properties: map[string]any{
				"cost":          result.Cost,
				"duration_ms":   result.DurationMS,
				"visited_nodes": result.VisitedNodes,
				"node_count":    len(result.Path),
			},
		}},
	}
	return json.Marshal(fc)
}

func init() { plugins.MustRegisterExporter(Exporter{}) }
