package astar_test

import (
	"fmt"
	"testing"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
)

// buildGridGraph builds an NxN grid of nodes spaced ~111m apart (0.001 deg)
// centered near Recife. Each interior node has 4 neighbors via bidirectional
// edges whose DistanceM is the real haversine distance.
//
// Node IDs are assigned row-major as: id = i*N + j + 1 (so 1..N*N).
// Total nodes: N*N. Total directed edges: ~4*N*(N-1).
func buildGridGraph(N int) (*graph.Graph, graph.NodeID, graph.NodeID) {
	g := graph.NewGraph()

	const baseLat = -8.05000
	const baseLon = -34.90000
	const step = 0.001 // ~111m in latitude, ~110m in lon at this latitude

	idOf := func(i, j int) graph.NodeID {
		return graph.NodeID(int64(i)*int64(N) + int64(j) + 1)
	}

	for i := 0; i < N; i++ {
		for j := 0; j < N; j++ {
			g.AddNode(idOf(i, j), baseLat+float64(i)*step, baseLon+float64(j)*step)
		}
	}

	for i := 0; i < N; i++ {
		for j := 0; j < N; j++ {
			here := g.Nodes[idOf(i, j)]
			if j+1 < N {
				right := g.Nodes[idOf(i, j+1)]
				d := geo.HaversineDistance(here.Lat, here.Lon, right.Lat, right.Lon)
				g.AddBidirectionalEdge(idOf(i, j), idOf(i, j+1), d)
			}
			if i+1 < N {
				down := g.Nodes[idOf(i+1, j)]
				d := geo.HaversineDistance(here.Lat, here.Lon, down.Lat, down.Lon)
				g.AddBidirectionalEdge(idOf(i, j), idOf(i+1, j), d)
			}
		}
	}

	source := idOf(0, 0)
	target := idOf(N-1, N-1)
	return g, source, target
}

// BenchmarkAStar_Grid measures pure in-memory A* query latency on a preloaded
// grid graph, excluding graph construction and any I/O. Heuristic is
// geo.HaversineHeuristic(1.4) — the same used by pkg/pathcraft/engine.
func BenchmarkAStar_Grid(b *testing.B) {
	sizes := []int{10, 50, 100, 200, 500}
	for _, n := range sizes {
		n := n
		b.Run(fmt.Sprintf("N=%d_nodes=%d", n, n*n), func(b *testing.B) {
			g, src, dst := buildGridGraph(n)
			h := geo.HaversineHeuristic(1.4)

			// Warm-up: make sure maps/slices are hot.
			if _, err := astar.AStar(g, src, dst, h); err != nil {
				b.Fatalf("warmup failed: %v", err)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				p, err := astar.AStar(g, src, dst, h)
				if err != nil {
					b.Fatalf("AStar: %v", err)
				}
				if p.NodesCount == 0 {
					b.Fatal("empty path")
				}
			}
		})
	}
}

// BenchmarkAStar_Grid_ZeroHeuristic is the same workload with h=0 (Dijkstra),
// so the delta vs BenchmarkAStar_Grid shows the heuristic's speedup.
func BenchmarkAStar_Grid_ZeroHeuristic(b *testing.B) {
	sizes := []int{50, 100, 200}
	zero := func(_, _ graph.Node) float64 { return 0 }
	for _, n := range sizes {
		n := n
		b.Run(fmt.Sprintf("N=%d_nodes=%d", n, n*n), func(b *testing.B) {
			g, src, dst := buildGridGraph(n)
			if _, err := astar.AStar(g, src, dst, zero); err != nil {
				b.Fatalf("warmup failed: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := astar.AStar(g, src, dst, zero); err != nil {
					b.Fatalf("AStar: %v", err)
				}
			}
		})
	}
}

func BenchmarkAStar_Contraction_LongChain(b *testing.B) {
	for _, contracted := range []bool{false, true} {
		name := "base"
		if contracted {
			name = "contracted"
		}
		b.Run(name, func(b *testing.B) {
			g := buildBenchmarkChain(10_000)
			if contracted {
				g.Contraction, _ = graph.BuildDegreeTwoContraction(g)
			}
			warmup, err := astar.AStar(g, 1, 10_000, zeroHeuristic)
			if err != nil {
				b.Fatalf("warmup failed: %v", err)
			}
			b.ReportMetric(float64(warmup.ExpandedNodes), "expanded/op")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := astar.AStar(g, 1, 10_000, zeroHeuristic); err != nil {
					b.Fatalf("AStar: %v", err)
				}
			}
		})
	}
}

var benchmarkContractionIndex *graph.ContractionIndex

func BenchmarkBuildDegreeTwoContraction_LongChain(b *testing.B) {
	g := buildBenchmarkChain(10_000)
	var index *graph.ContractionIndex
	var stats graph.ContractionStats
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		index, stats = graph.BuildDegreeTwoContraction(g)
	}
	b.StopTimer()
	b.ReportMetric(float64(stats.ContractedNodes), "contracted_nodes")
	b.ReportMetric(float64(stats.Chains), "chains")
	benchmarkContractionIndex = index
}

func buildBenchmarkChain(nodes int) *graph.Graph {
	g := graph.NewGraph()
	for id := 1; id <= nodes; id++ {
		g.AddNode(graph.NodeID(id), 0, float64(id)/100_000)
		if id > 1 {
			g.AddBidirectionalEdge(graph.NodeID(id-1), graph.NodeID(id), 1)
		}
	}
	return g
}
