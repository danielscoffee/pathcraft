package plugins_e2e_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/osmgraph"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/registry"

	_ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/astar"
	_ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/geojson"
	_ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/osm"
)

func TestOSMAstarGeoJSONPipeline(t *testing.T) {
	ctx := context.Background()

	loader, ok := registry.Default.Loader("osm")
	if !ok {
		t.Fatal("osm loader not registered")
	}
	g, err := loader.Load(ctx, "../../../testdata/example.osm")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if sz, ok := g.(core.Sized); !ok || sz.NodeCount() == 0 {
		t.Fatalf("graph empty or unsized")
	}

	from, to, ok := pickConnectedPair(g)
	if !ok {
		t.Skip("no connected node pair found in example.osm")
	}

	algo, _ := registry.Default.Algorithm("astar")
	result, err := algo.Route(ctx, g, core.RouteRequest{From: from, To: to})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if len(result.Path) < 2 {
		t.Fatalf("expected non-trivial path, got %d nodes", len(result.Path))
	}

	exp, _ := registry.Default.Exporter("geojson")
	out, err := exp.Export(ctx, result, g)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if parsed["type"] != "FeatureCollection" {
		t.Fatalf("expected FeatureCollection, got %v", parsed["type"])
	}
}

func pickConnectedPair(g core.Graph) (core.NodeID, core.NodeID, bool) {
	native, ok := g.(osmgraph.Native)
	if !ok {
		return "", "", false
	}
	ig := native.NativeGraph()
	for id := range ig.Nodes {
		neighbors := ig.Neighbors(id)
		if len(neighbors) > 0 {
			return osmgraph.FormatID(id), osmgraph.FormatID(neighbors[0].To), true
		}
	}
	return "", "", false
}
