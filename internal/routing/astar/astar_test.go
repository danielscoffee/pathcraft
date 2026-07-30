package astar_test

import (
	"context"
	"errors"
	"testing"

	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
)

// buildTestGraph creates a simple grid-like graph:
//
//	1 --- 2 --- 3
//	|     |     |
//	4 --- 5 --- 6
//	|     |     |
//	7 --- 8 --- 9
func buildTestGraph() *graph.Graph {
	g := graph.NewGraph()

	for i := 1; i <= 9; i++ {
		g.AddNode(graph.NodeID(i), 0, 0)
	}

	g.AddBidirectionalEdge(1, 2, 1.0)
	g.AddBidirectionalEdge(2, 3, 1.0)
	g.AddBidirectionalEdge(4, 5, 1.0)
	g.AddBidirectionalEdge(5, 6, 1.0)
	g.AddBidirectionalEdge(7, 8, 1.0)
	g.AddBidirectionalEdge(8, 9, 1.0)

	g.AddBidirectionalEdge(1, 4, 1.0)
	g.AddBidirectionalEdge(4, 7, 1.0)
	g.AddBidirectionalEdge(2, 5, 1.0)
	g.AddBidirectionalEdge(5, 8, 1.0)
	g.AddBidirectionalEdge(3, 6, 1.0)
	g.AddBidirectionalEdge(6, 9, 1.0)

	return g
}

func zeroHeuristic(_, _ graph.Node) float64 {
	return 0
}

func TestAStar_SimplePathExists(t *testing.T) {
	g := buildTestGraph()

	path, err := astar.AStar(g, 1, 9, zeroHeuristic)
	if err != nil {
		t.Fatalf("expected path, got error: %v", err)
	}

	expectedCost := 4.0
	if path.TotalCost != expectedCost {
		t.Errorf("expected cost %v, got %v", expectedCost, path.TotalCost)
	}

	if path.NodesCount != 5 {
		t.Errorf("expected 5 nodes, got %d", path.NodesCount)
	}

	if path.Nodes[0] != 1 {
		t.Errorf("expected path to start at 1, got %d", path.Nodes[0])
	}
	if path.Nodes[len(path.Nodes)-1] != 9 {
		t.Errorf("expected path to end at 9, got %d", path.Nodes[len(path.Nodes)-1])
	}
}

func TestAStarWithProfileContextHonorsPreCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := astar.AStarWithProfileContext(ctx, buildTestGraph(), 1, 9, zeroHeuristic, mobility.NewWalking(1.4))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AStarWithProfileContext() error = %v, want context.Canceled", err)
	}
}

func TestAStarWithProfileContextCancelsDuringSearch(t *testing.T) {
	const nodes = 2_000
	g := graph.NewGraph()
	for id := graph.NodeID(1); id <= nodes; id++ {
		g.AddNode(id, 0, float64(id)/10_000)
		if id > 1 {
			g.AddBidirectionalEdge(id-1, id, 1)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	heuristicCalls := 0
	heuristic := func(_, _ graph.Node) float64 {
		heuristicCalls++
		if heuristicCalls == 100 {
			cancel()
		}
		return 0
	}
	_, err := astar.AStarWithProfileContext(ctx, g, 1, nodes, heuristic, mobility.NewWalking(1.4))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AStarWithProfileContext() error = %v, want context.Canceled", err)
	}
}

func TestAStarWithProfileBlocksDrivingOnNonCarEdges(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(1, 0, 0)
	g.AddNode(2, 0, 0.001)
	g.AddRestrictedEdgeWithMeta(1, 2, 10, "footway", "walk only", graph.RestrictedDriving)

	if _, err := astar.AStarWithProfile(g, 1, 2, zeroHeuristic, mobility.NewWalking(1.4)); err != nil {
		t.Fatalf("walking on footway should be allowed, got %v", err)
	}
	if _, err := astar.AStarWithProfile(g, 1, 2, zeroHeuristic, mobility.NewDriving(8.3)); err == nil {
		t.Fatal("driving on footway should be blocked")
	}
}

func TestAStarWithProfileHonorsDrivingOnewayRestrictions(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(1, 0, 0)
	g.AddNode(2, 0, 0.001)
	g.AddEdgeWithMeta(1, 2, 10, "residential", "oneway")
	g.AddRestrictedEdgeWithMeta(2, 1, 10, "residential", "oneway", graph.RestrictedDriving)

	if _, err := astar.AStarWithProfile(g, 2, 1, zeroHeuristic, mobility.NewWalking(1.4)); err != nil {
		t.Fatalf("walking reverse oneway should be allowed, got %v", err)
	}
	if _, err := astar.AStarWithProfile(g, 2, 1, zeroHeuristic, mobility.NewDriving(8.3)); err == nil {
		t.Fatal("driving reverse oneway should be blocked")
	}
}

func TestAStar_SameSourceAndTarget(t *testing.T) {
	g := buildTestGraph()

	path, err := astar.AStar(g, 5, 5, zeroHeuristic)
	if err != nil {
		t.Fatalf("expected path, got error: %v", err)
	}

	if path.TotalCost != 0 {
		t.Errorf("expected cost 0, got %v", path.TotalCost)
	}

	if path.NodesCount != 1 {
		t.Errorf("expected 1 node, got %d", path.NodesCount)
	}
}

func TestAStar_AdjacentNodes(t *testing.T) {
	g := buildTestGraph()

	path, err := astar.AStar(g, 1, 2, zeroHeuristic)
	if err != nil {
		t.Fatalf("expected path, got error: %v", err)
	}

	if path.TotalCost != 1.0 {
		t.Errorf("expected cost 1.0, got %v", path.TotalCost)
	}

	if path.NodesCount != 2 {
		t.Errorf("expected 2 nodes, got %d", path.NodesCount)
	}

	expected := []graph.NodeID{1, 2}
	for i, nodeID := range path.Nodes {
		if nodeID != expected[i] {
			t.Errorf("at position %d: expected %d, got %d", i, expected[i], nodeID)
		}
	}
}

func TestAStar_NodeNotFound(t *testing.T) {
	g := buildTestGraph()

	_, err := astar.AStar(g, 1, 100, zeroHeuristic)
	if err != astar.ErrNodeNotFound {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}

	_, err = astar.AStar(g, 100, 1, zeroHeuristic)
	if err != astar.ErrNodeNotFound {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestAStar_NoPathExists(t *testing.T) {
	g := graph.NewGraph()

	g.AddNode(1, 0, 0)
	g.AddNode(2, 0, 0)
	g.AddBidirectionalEdge(1, 2, 1.0)

	g.AddNode(3, 0, 0)
	g.AddNode(4, 0, 0)
	g.AddBidirectionalEdge(3, 4, 1.0)

	_, err := astar.AStar(g, 1, 4, zeroHeuristic)
	if err != astar.ErrNoPath {
		t.Errorf("expected ErrNoPath, got %v", err)
	}
}

type penalizedProfile struct {
	mobility.Profile
	penalties map[string]float64
}

func (p penalizedProfile) HighwayPenalty(highway string) float64 {
	if penalty, ok := p.penalties[highway]; ok {
		return penalty
	}
	return 1
}

func TestAStarWithProfileAppliesHighwayPenalties(t *testing.T) {
	g := graph.NewGraph()
	for id := graph.NodeID(1); id <= 4; id++ {
		g.AddNode(id, 0, 0)
	}
	g.AddEdgeWithMeta(1, 2, 50, "primary", "Fast Road")
	g.AddEdgeWithMeta(2, 4, 50, "primary", "Fast Road")
	g.AddEdgeWithMeta(1, 3, 75, "residential", "Calm Road")
	g.AddEdgeWithMeta(3, 4, 75, "residential", "Calm Road")

	profile := penalizedProfile{
		Profile:   mobility.NewWalking(2),
		penalties: map[string]float64{"primary": 2},
	}
	path, err := astar.AStarWithProfile(g, 1, 4, zeroHeuristic, profile)
	if err != nil {
		t.Fatalf("AStarWithProfile() error = %v", err)
	}
	want := []graph.NodeID{1, 3, 4}
	if len(path.Nodes) != len(want) {
		t.Fatalf("Nodes = %v, want %v", path.Nodes, want)
	}
	for i := range want {
		if path.Nodes[i] != want[i] {
			t.Fatalf("Nodes = %v, want %v", path.Nodes, want)
		}
	}
	if path.TotalCost != 150 {
		t.Fatalf("TotalCost = %v, want 150", path.TotalCost)
	}
	if path.TotalDistance != 150 {
		t.Fatalf("TotalDistance = %v, want 150", path.TotalDistance)
	}
}

func TestAStar_WeightedEdges(t *testing.T) {
	g := graph.NewGraph()

	// Graph with weighted edges:
	//     2
	//    / \
	//   1   4      (1->2->4 costs 10, 1->3->4 costs 4)
	//    \ /
	//     3
	g.AddNode(1, 0, 0)
	g.AddNode(2, 0, 0)
	g.AddNode(3, 0, 0)
	g.AddNode(4, 0, 0)

	g.AddEdge(1, 2, 5.0)
	g.AddEdge(2, 4, 5.0)
	g.AddEdge(1, 3, 2.0)
	g.AddEdge(3, 4, 2.0)

	path, err := astar.AStar(g, 1, 4, zeroHeuristic)
	if err != nil {
		t.Fatalf("expected path, got error: %v", err)
	}

	expectedCost := 4.0
	if path.TotalCost != expectedCost {
		t.Errorf("expected cost %v, got %v", expectedCost, path.TotalCost)
	}

	expectedPath := []graph.NodeID{1, 3, 4}
	if len(path.Nodes) != len(expectedPath) {
		t.Fatalf("expected %d nodes, got %d", len(expectedPath), len(path.Nodes))
	}
	for i, nodeID := range path.Nodes {
		if nodeID != expectedPath[i] {
			t.Errorf("at position %d: expected %d, got %d", i, expectedPath[i], nodeID)
		}
	}
}
