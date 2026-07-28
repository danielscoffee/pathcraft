package graph

import (
	"reflect"
	"slices"
	"testing"
)

func TestBuildDegreeTwoContractionBuildsDeterministicDirectedChains(t *testing.T) {
	build := func(reverse bool) *Graph {
		g := NewGraph()
		for id := NodeID(1); id <= 5; id++ {
			g.AddNode(id, 0, float64(id))
		}

		edges := []struct {
			from, to   NodeID
			highway    string
			restricted []RestrictedMode
		}{
			{1, 2, "residential", nil},
			{2, 1, "residential", nil},
			{2, 3, "primary", nil},
			{3, 2, "primary", nil},
			{3, 4, "primary", []RestrictedMode{RestrictedDriving}},
			{4, 3, "primary", []RestrictedMode{RestrictedDriving}},
			{4, 5, "residential", nil},
			{5, 4, "residential", nil},
		}
		if reverse {
			slices.Reverse(edges)
		}
		for _, edge := range edges {
			g.AddRestrictedEdgeWithMeta(edge.from, edge.to, 10, edge.highway, "", edge.restricted...)
		}
		return g
	}

	first, firstStats := BuildDegreeTwoContraction(build(false))
	second, secondStats := BuildDegreeTwoContraction(build(true))
	if !reflect.DeepEqual(first, second) || firstStats != secondStats {
		t.Fatalf("contraction differs by insertion order:\nfirst=%+v (%+v)\nsecond=%+v (%+v)", first, firstStats, second, secondStats)
	}
	if firstStats.ContractedNodes != 3 || firstStats.Chains != 2 {
		t.Fatalf("stats = %+v, want 3 contracted nodes and 2 directed chains", firstStats)
	}
	if !first.Retained[1] || !first.Retained[5] || first.Retained[2] || first.Retained[3] || first.Retained[4] {
		t.Fatalf("retained nodes = %v, want only endpoints 1 and 5", first.Retained)
	}

	forward := findContractionChain(t, first, 1, 5)
	if !slices.Equal(forward.Nodes, []NodeID{1, 2, 3, 4, 5}) {
		t.Fatalf("forward nodes = %v", forward.Nodes)
	}
	if forward.DistanceM != 40 {
		t.Fatalf("forward distance = %v, want 40", forward.DistanceM)
	}
	if !reflect.DeepEqual(forward.CostComponents, []CostComponent{{Highway: "primary", DistanceM: 20}, {Highway: "residential", DistanceM: 20}}) {
		t.Fatalf("cost components = %+v", forward.CostComponents)
	}
	if !slices.Equal(forward.RestrictedModes, []RestrictedMode{RestrictedDriving}) {
		t.Fatalf("restricted modes = %v", forward.RestrictedModes)
	}

	positions := first.Positions[3]
	if len(positions) != 2 {
		t.Fatalf("node 3 positions = %+v, want one per direction", positions)
	}
}

func TestBuildDegreeTwoContractionRetainsIntersectionsAndAmbiguousNodes(t *testing.T) {
	g := NewGraph()
	for id := NodeID(1); id <= 5; id++ {
		g.AddNode(id, 0, float64(id))
	}
	g.AddBidirectionalEdge(1, 2, 1)
	g.AddBidirectionalEdge(2, 3, 1)
	g.AddBidirectionalEdge(3, 4, 1)
	g.AddBidirectionalEdge(2, 5, 1)

	index, stats := BuildDegreeTwoContraction(g)
	if !index.Retained[2] || !index.Retained[4] || index.Retained[3] {
		t.Fatalf("retained nodes = %v, want intersection 2 and endpoint 4 retained, node 3 contracted", index.Retained)
	}
	if stats.ContractedNodes != 1 {
		t.Fatalf("contracted nodes = %d, want 1", stats.ContractedNodes)
	}
	chain := findContractionChain(t, index, 2, 4)
	if !slices.Equal(chain.Nodes, []NodeID{2, 3, 4}) {
		t.Fatalf("chain nodes = %v", chain.Nodes)
	}

	ambiguous := NewGraph()
	for id := NodeID(1); id <= 3; id++ {
		ambiguous.AddNode(id, 0, float64(id))
	}
	ambiguous.AddBidirectionalEdge(1, 2, 1)
	ambiguous.AddEdge(1, 2, 2)
	ambiguous.AddBidirectionalEdge(2, 3, 1)
	ambiguousIndex, ambiguousStats := BuildDegreeTwoContraction(ambiguous)
	if !ambiguousIndex.Retained[2] || ambiguousStats.ContractedNodes != 0 {
		t.Fatalf("ambiguous parallel node contracted: retained=%v stats=%+v", ambiguousIndex.Retained, ambiguousStats)
	}
}

func TestBuildDegreeTwoContractionLeavesPureCyclesUncontracted(t *testing.T) {
	g := NewGraph()
	for id := NodeID(1); id <= 3; id++ {
		g.AddNode(id, 0, float64(id))
	}
	g.AddBidirectionalEdge(1, 2, 1)
	g.AddBidirectionalEdge(2, 3, 1)
	g.AddBidirectionalEdge(3, 1, 1)

	index, stats := BuildDegreeTwoContraction(g)
	if stats.ContractedNodes != 0 {
		t.Fatalf("pure cycle stats = %+v, want no contraction", stats)
	}
	for id := NodeID(1); id <= 3; id++ {
		if !index.Retained[id] {
			t.Fatalf("cycle node %d was contracted", id)
		}
	}
	for _, chain := range index.Chains {
		if len(chain.Nodes) != 2 {
			t.Fatalf("cycle chain = %v, want base edge only", chain.Nodes)
		}
	}
}

func TestGraphMutationInvalidatesContraction(t *testing.T) {
	g := NewGraph()
	g.AddNode(1, 0, 0)
	g.AddNode(2, 0, 1)
	g.AddBidirectionalEdge(1, 2, 1)
	g.Contraction, _ = BuildDegreeTwoContraction(g)

	g.AddNode(3, 0, 2)
	if g.Contraction != nil {
		t.Fatal("AddNode did not invalidate contraction")
	}

	g.Contraction, _ = BuildDegreeTwoContraction(g)
	g.AddEdge(2, 3, 1)
	if g.Contraction != nil {
		t.Fatal("AddEdge did not invalidate contraction")
	}
}

func findContractionChain(t *testing.T, index *ContractionIndex, from, to NodeID) ContractionChain {
	t.Helper()
	for _, chainID := range index.Out[from] {
		chain := index.Chains[chainID]
		if chain.Nodes[len(chain.Nodes)-1] == to {
			return chain
		}
	}
	t.Fatalf("chain %d -> %d not found", from, to)
	return ContractionChain{}
}
