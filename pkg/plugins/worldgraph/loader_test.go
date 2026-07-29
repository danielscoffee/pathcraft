package worldgraph

import (
	"context"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

func TestLoaderRegistersAndOpensLazyWorldGraph(t *testing.T) {
	if (Loader{}).Name() != "worldgraph" {
		t.Fatalf("Loader.Name() = %q", (Loader{}).Name())
	}
	registered, ok := plugins.Default.Loader("worldgraph")
	if !ok {
		t.Fatal("worldgraph loader is not registered")
	}
	tile := TileID{Z: 4, X: 8, Y: 8}
	from := routerNode(tile, 1, 0.25, 0.5)
	to := routerNode(tile, 2, 0.75, 0.5)
	edge := routerDirectedEdge(t, from, to, 70, false, false)
	dir, _ := publishRouterFixture(t, map[TileID]Chunk{tile: {Tile: tile, Nodes: []Node{from, to}, Edges: []Edge{edge}}})
	loaded, err := registered.Load(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.(*Router); !ok {
		t.Fatalf("loaded graph type = %T, want *Router", loaded)
	}
	if _, ok := loaded.(interface {
		RouteByCoordinatesContext(context.Context, engine.CoordinateRouteRequest) (*engine.CoordinateRouteResult, error)
	}); !ok {
		t.Fatal("loaded world graph lacks context-aware coordinate routing")
	}
	edges, err := loaded.Neighbors(context.Background(), core.NodeID("1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0].From != "1" || edges[0].To != "2" || edges[0].Cost != edge.DistanceMeters {
		t.Fatalf("Neighbors() = %+v", edges)
	}
	if closer, ok := loaded.(interface{ Close() error }); !ok {
		t.Fatal("loaded world graph is not closable")
	} else if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLoaderRejectsMissingSourceAndCancellation(t *testing.T) {
	if _, err := (Loader{}).Load(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("missing source error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Loader{}).Load(ctx, t.TempDir()); err != context.Canceled {
		t.Fatalf("cancelled Load() error = %v", err)
	}
}

var _ core.GraphLoader = Loader{}
