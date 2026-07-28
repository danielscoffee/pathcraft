package graph

import (
	"cmp"
	"slices"
)

const ContractionVersion = 1

type CostComponent struct {
	Highway   string
	DistanceM float64
}

type ContractionChain struct {
	Nodes           []NodeID
	Segments        []Edge
	DistanceM       float64
	CostComponents  []CostComponent
	RestrictedModes []RestrictedMode
}

type ContractionPosition struct {
	Chain  int
	Offset int
}

type ContractionIndex struct {
	Version   int
	Retained  map[NodeID]bool
	Chains    []ContractionChain
	Out       map[NodeID][]int
	Positions map[NodeID][]ContractionPosition
}

type ContractionStats struct {
	ContractedNodes int
	Chains          int
}

// BuildDegreeTwoContraction builds directed chains without changing base graph.
// Only unambiguous degree-two nodes contract; all original nodes remain routable.
func BuildDegreeTwoContraction(g *Graph) (*ContractionIndex, ContractionStats) {
	index := &ContractionIndex{
		Version:   ContractionVersion,
		Retained:  make(map[NodeID]bool, len(g.Nodes)),
		Out:       make(map[NodeID][]int),
		Positions: make(map[NodeID][]ContractionPosition, len(g.Nodes)),
	}
	if len(g.Nodes) == 0 {
		return index, ContractionStats{}
	}

	nodeIDs := sortedNodeIDs(g.Nodes)
	neighbors, outgoing, incoming, unsafe := contractionTopology(g)
	for _, id := range nodeIDs {
		if unsafe[id] || len(neighbors[id]) != 2 {
			index.Retained[id] = true
			continue
		}
		for neighbor := range neighbors[id] {
			if outgoing[id][neighbor] != 1 || incoming[id][neighbor] != 1 {
				index.Retained[id] = true
				break
			}
		}
	}
	retainSmallComponents(nodeIDs, neighbors, index.Retained)

	for _, start := range nodeIDs {
		if !index.Retained[start] {
			continue
		}
		for _, first := range sortedEdges(g.Edges[start]) {
			chain := ContractionChain{
				Nodes:    []NodeID{start, first.To},
				Segments: []Edge{first},
			}
			previous, current := start, first.To
			seen := map[NodeID]bool{start: true}

			for !index.Retained[current] && !seen[current] {
				seen[current] = true
				next := otherNeighbor(neighbors[current], previous)
				nextEdge, ok := uniqueEdgeTo(g.Edges[current], next)
				if !ok {
					break
				}
				chain.Nodes = append(chain.Nodes, next)
				chain.Segments = append(chain.Segments, nextEdge)
				previous, current = current, next
			}

			if !index.Retained[current] {
				continue
			}
			summarizeChain(&chain)
			chainID := len(index.Chains)
			index.Chains = append(index.Chains, chain)
			index.Out[start] = append(index.Out[start], chainID)
			for offset, id := range chain.Nodes {
				index.Positions[id] = append(index.Positions[id], ContractionPosition{Chain: chainID, Offset: offset})
			}
		}
	}

	contracted := 0
	for _, id := range nodeIDs {
		if !index.Retained[id] {
			contracted++
		}
	}
	return index, ContractionStats{ContractedNodes: contracted, Chains: len(index.Chains)}
}

func contractionTopology(g *Graph) (
	map[NodeID]map[NodeID]struct{},
	map[NodeID]map[NodeID]int,
	map[NodeID]map[NodeID]int,
	map[NodeID]bool,
) {
	neighbors := make(map[NodeID]map[NodeID]struct{}, len(g.Nodes))
	outgoing := make(map[NodeID]map[NodeID]int, len(g.Nodes))
	incoming := make(map[NodeID]map[NodeID]int, len(g.Nodes))
	unsafe := make(map[NodeID]bool)
	for id := range g.Nodes {
		neighbors[id] = make(map[NodeID]struct{})
		outgoing[id] = make(map[NodeID]int)
		incoming[id] = make(map[NodeID]int)
	}
	for from, edges := range g.Edges {
		if _, ok := g.Nodes[from]; !ok {
			continue
		}
		for _, edge := range edges {
			if _, ok := g.Nodes[edge.To]; !ok || edge.To == from {
				unsafe[from] = true
				continue
			}
			neighbors[from][edge.To] = struct{}{}
			neighbors[edge.To][from] = struct{}{}
			outgoing[from][edge.To]++
			incoming[edge.To][from]++
		}
	}
	return neighbors, outgoing, incoming, unsafe
}

func retainSmallComponents(nodeIDs []NodeID, neighbors map[NodeID]map[NodeID]struct{}, retained map[NodeID]bool) {
	visited := make(map[NodeID]bool, len(nodeIDs))
	for _, root := range nodeIDs {
		if visited[root] {
			continue
		}
		component := []NodeID{root}
		visited[root] = true
		retainedCount := 0
		for i := 0; i < len(component); i++ {
			id := component[i]
			if retained[id] {
				retainedCount++
			}
			for neighbor := range neighbors[id] {
				if !visited[neighbor] {
					visited[neighbor] = true
					component = append(component, neighbor)
				}
			}
		}
		if retainedCount >= 2 {
			continue
		}
		for _, id := range component {
			retained[id] = true
		}
	}
}

func sortedNodeIDs(nodes map[NodeID]Node) []NodeID {
	ids := make([]NodeID, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func sortedEdges(edges []Edge) []Edge {
	result := make([]Edge, len(edges))
	for i, edge := range edges {
		result[i] = edge
		result[i].RestrictedModes = append([]RestrictedMode(nil), edge.RestrictedModes...)
		slices.Sort(result[i].RestrictedModes)
	}
	slices.SortFunc(result, func(a, b Edge) int {
		if n := cmp.Compare(a.To, b.To); n != 0 {
			return n
		}
		if n := cmp.Compare(a.DistanceM, b.DistanceM); n != 0 {
			return n
		}
		if n := cmp.Compare(a.Highway, b.Highway); n != 0 {
			return n
		}
		if n := cmp.Compare(a.Name, b.Name); n != 0 {
			return n
		}
		return slices.Compare(a.RestrictedModes, b.RestrictedModes)
	})
	return result
}

func otherNeighbor(neighbors map[NodeID]struct{}, previous NodeID) NodeID {
	for neighbor := range neighbors {
		if neighbor != previous {
			return neighbor
		}
	}
	return previous
}

func uniqueEdgeTo(edges []Edge, target NodeID) (Edge, bool) {
	var match Edge
	found := false
	for _, edge := range edges {
		if edge.To != target {
			continue
		}
		if found {
			return Edge{}, false
		}
		match = edge
		found = true
	}
	if !found {
		return Edge{}, false
	}
	match.RestrictedModes = append([]RestrictedMode(nil), match.RestrictedModes...)
	slices.Sort(match.RestrictedModes)
	return match, true
}

func summarizeChain(chain *ContractionChain) {
	distances := make(map[string]float64)
	restrictions := make(map[RestrictedMode]struct{})
	for _, edge := range chain.Segments {
		chain.DistanceM += edge.DistanceM
		distances[edge.Highway] += edge.DistanceM
		for _, mode := range edge.RestrictedModes {
			restrictions[mode] = struct{}{}
		}
	}
	highways := make([]string, 0, len(distances))
	for highway := range distances {
		highways = append(highways, highway)
	}
	slices.Sort(highways)
	chain.CostComponents = make([]CostComponent, 0, len(highways))
	for _, highway := range highways {
		chain.CostComponents = append(chain.CostComponents, CostComponent{Highway: highway, DistanceM: distances[highway]})
	}
	chain.RestrictedModes = make([]RestrictedMode, 0, len(restrictions))
	for mode := range restrictions {
		chain.RestrictedModes = append(chain.RestrictedModes, mode)
	}
	slices.Sort(chain.RestrictedModes)
}
