package astar_test

import (
	"testing"

	"github.com/danielscoffee/pathcraft/internal/domain/graph"
	astar "github.com/danielscoffee/pathcraft/internal/domain/routing/astar"
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
