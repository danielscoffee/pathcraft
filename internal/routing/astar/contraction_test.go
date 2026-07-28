package astar_test

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
)

func TestAStarContractionMatchesBaseForEveryEndpoint(t *testing.T) {
	g := contractionTestGraph()
	index, stats := graph.BuildDegreeTwoContraction(g)
	if stats.ContractedNodes == 0 {
		t.Fatal("test graph did not produce contracted nodes")
	}

	profiles := []struct {
		name    string
		profile mobility.Profile
	}{
		{name: "walking", profile: mobility.NewWalking(1.4)},
		{name: "driving", profile: mobility.NewDriving(8.3)},
		{name: "penalties", profile: penalizedProfile{
			Profile:   mobility.NewWalking(1.4),
			penalties: map[string]float64{"primary": 2},
		}},
	}

	for _, test := range profiles {
		t.Run(test.name, func(t *testing.T) {
			for source := graph.NodeID(1); source <= 7; source++ {
				for target := graph.NodeID(1); target <= 7; target++ {
					g.Contraction = nil
					base, baseErr := astar.AStarWithProfile(g, source, target, zeroHeuristic, test.profile)
					g.Contraction = index
					contracted, contractedErr := astar.AStarWithProfile(g, source, target, zeroHeuristic, test.profile)

					if !errors.Is(contractedErr, baseErr) || !errors.Is(baseErr, contractedErr) {
						t.Fatalf("%d -> %d errors differ: base=%v contracted=%v", source, target, baseErr, contractedErr)
					}
					if baseErr != nil {
						continue
					}
					if !slices.Equal(contracted.Nodes, base.Nodes) {
						t.Fatalf("%d -> %d nodes: base=%v contracted=%v", source, target, base.Nodes, contracted.Nodes)
					}
					if math.Abs(contracted.TotalCost-base.TotalCost) > 1e-9 || math.Abs(contracted.TotalDistance-base.TotalDistance) > 1e-9 {
						t.Fatalf("%d -> %d totals: base=%+v contracted=%+v", source, target, base, contracted)
					}
				}
			}
		})
	}
}

func TestAStarContractionUsesChainForInteriorEndpoints(t *testing.T) {
	g := graph.NewGraph()
	for id := graph.NodeID(1); id <= 100; id++ {
		g.AddNode(id, 0, float64(id)/1000)
		if id > 1 {
			g.AddBidirectionalEdge(id-1, id, 1)
		}
	}

	g.Contraction = nil
	base, err := astar.AStar(g, 20, 80, zeroHeuristic)
	if err != nil {
		t.Fatalf("base AStar() error = %v", err)
	}
	g.Contraction, _ = graph.BuildDegreeTwoContraction(g)
	contracted, err := astar.AStar(g, 20, 80, zeroHeuristic)
	if err != nil {
		t.Fatalf("contracted AStar() error = %v", err)
	}

	if !slices.Equal(contracted.Nodes, base.Nodes) {
		t.Fatalf("contracted nodes = %v, want %v", contracted.Nodes, base.Nodes)
	}
	if contracted.ExpandedNodes >= base.ExpandedNodes/2 {
		t.Fatalf("expanded nodes: contracted=%d base=%d, want contraction to skip chain", contracted.ExpandedNodes, base.ExpandedNodes)
	}
}

func TestAStarCustomPenaltyUsesAdmissibleHeuristic(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(1, 0, 0)
	g.AddNode(2, 10, 0)
	g.AddNode(3, 0, 0.001)
	g.AddEdgeWithMeta(1, 3, 100, "residential", "direct")
	g.AddEdgeWithMeta(1, 2, 1000, "discount", "detour")
	g.AddEdgeWithMeta(2, 3, 1, "discount", "detour")

	profile := penalizedProfile{
		Profile: mobility.NewWalking(1),
		penalties: map[string]float64{
			"discount": 0.01,
		},
	}
	path, err := astar.AStarWithProfile(g, 1, 3, geo.HaversineHeuristic(1), profile)
	if err != nil {
		t.Fatalf("AStarWithProfile() error = %v", err)
	}
	if !slices.Equal(path.Nodes, []graph.NodeID{1, 2, 3}) {
		t.Fatalf("nodes = %v, want discounted detour", path.Nodes)
	}
	if math.Abs(path.TotalCost-10.01) > 1e-9 {
		t.Fatalf("cost = %v, want 10.01", path.TotalCost)
	}
}

func contractionTestGraph() *graph.Graph {
	g := graph.NewGraph()
	for id := graph.NodeID(1); id <= 7; id++ {
		g.AddNode(id, 0, float64(id)/1000)
	}
	add := func(a, b graph.NodeID, distance float64, highway string, restrictions ...graph.RestrictedMode) {
		g.AddRestrictedEdgeWithMeta(a, b, distance, highway, "", restrictions...)
		g.AddRestrictedEdgeWithMeta(b, a, distance, highway, "", restrictions...)
	}
	add(1, 2, 10, "residential")
	add(2, 3, 10, "primary")
	add(3, 4, 10, "primary", graph.RestrictedDriving)
	add(4, 5, 10, "residential")
	add(3, 6, 15, "residential")
	add(6, 7, 15, "residential")
	return g
}
