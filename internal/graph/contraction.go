package graph

import (
	"cmp"
	"fmt"
	"math"
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

type ContractionArc struct {
	Chain int
	From  int
	To    int
}

// Outgoing returns contracted arcs needed for this query. Prefix, suffix, and
// direct subchain arcs preserve arbitrary original-node endpoints.
func (index *ContractionIndex) Outgoing(node, source, target NodeID) []ContractionArc {
	if index == nil || index.Version != ContractionVersion {
		return nil
	}

	arcs := make([]ContractionArc, 0, len(index.Out[node])+4)
	add := func(arc ContractionArc) {
		if arc.From >= arc.To {
			return
		}
		for _, existing := range arcs {
			if existing == arc {
				return
			}
		}
		arcs = append(arcs, arc)
	}

	for _, chainID := range index.Out[node] {
		add(ContractionArc{Chain: chainID, To: len(index.Chains[chainID].Nodes) - 1})
	}
	if node == source {
		for _, position := range index.Positions[source] {
			add(ContractionArc{
				Chain: position.Chain,
				From:  position.Offset,
				To:    len(index.Chains[position.Chain].Nodes) - 1,
			})
		}
	}
	for _, position := range index.Positions[target] {
		chain := index.Chains[position.Chain]
		if chain.Nodes[0] == node {
			add(ContractionArc{Chain: position.Chain, To: position.Offset})
		}
	}
	if node == source {
		for _, from := range index.Positions[source] {
			for _, to := range index.Positions[target] {
				if from.Chain == to.Chain && from.Offset < to.Offset {
					add(ContractionArc{Chain: from.Chain, From: from.Offset, To: to.Offset})
				}
			}
		}
	}
	return arcs
}

func (index *ContractionIndex) validate(g *Graph) error {
	if index.Version != ContractionVersion {
		return fmt.Errorf("unsupported contraction version %d, want %d", index.Version, ContractionVersion)
	}
	for id, retained := range index.Retained {
		if !retained {
			return fmt.Errorf("contraction retained node %d has false marker", id)
		}
		if !g.HasNode(id) {
			return fmt.Errorf("contraction retained node %d is missing", id)
		}
	}

	outSeen := make([]bool, len(index.Chains))
	for from, chainIDs := range index.Out {
		if !index.Retained[from] {
			return fmt.Errorf("contraction output node %d is not retained", from)
		}
		for _, chainID := range chainIDs {
			if chainID < 0 || chainID >= len(index.Chains) {
				return fmt.Errorf("contraction output node %d references chain %d", from, chainID)
			}
			if chain := index.Chains[chainID]; len(chain.Nodes) == 0 || chain.Nodes[0] != from {
				return fmt.Errorf("contraction chain %d does not start at %d", chainID, from)
			}
			outSeen[chainID] = true
		}
	}

	for node, positions := range index.Positions {
		if !g.HasNode(node) {
			return fmt.Errorf("contraction position node %d is missing", node)
		}
		for _, position := range positions {
			if position.Chain < 0 || position.Chain >= len(index.Chains) {
				return fmt.Errorf("contraction node %d references chain %d", node, position.Chain)
			}
			chain := index.Chains[position.Chain]
			if position.Offset < 0 || position.Offset >= len(chain.Nodes) || chain.Nodes[position.Offset] != node {
				return fmt.Errorf("contraction node %d has invalid chain %d offset %d", node, position.Chain, position.Offset)
			}
		}
	}

	for chainID, chain := range index.Chains {
		if !outSeen[chainID] {
			return fmt.Errorf("contraction chain %d is not reachable from output index", chainID)
		}
		if len(chain.Nodes) < 2 || len(chain.Segments) != len(chain.Nodes)-1 {
			return fmt.Errorf("contraction chain %d has %d nodes and %d segments", chainID, len(chain.Nodes), len(chain.Segments))
		}
		if !index.Retained[chain.Nodes[0]] || !index.Retained[chain.Nodes[len(chain.Nodes)-1]] {
			return fmt.Errorf("contraction chain %d endpoints are not retained", chainID)
		}
		distance := 0.0
		for offset, node := range chain.Nodes {
			if !g.HasNode(node) {
				return fmt.Errorf("contraction chain %d node %d is missing", chainID, node)
			}
			if !hasContractionPosition(index.Positions[node], chainID, offset) {
				return fmt.Errorf("contraction chain %d node %d lacks position %d", chainID, node, offset)
			}
			if offset == len(chain.Segments) {
				continue
			}
			segment := chain.Segments[offset]
			if segment.To != chain.Nodes[offset+1] || invalidDistance(segment.DistanceM) ||
				!matchesBaseEdge(g.Edges[node], segment) {
				return fmt.Errorf("contraction chain %d has invalid segment %d", chainID, offset)
			}
			distance += segment.DistanceM
		}
		if invalidDistance(chain.DistanceM) || chain.DistanceM != distance {
			return fmt.Errorf("contraction chain %d distance is invalid", chainID)
		}
		componentDistance := 0.0
		for _, component := range chain.CostComponents {
			if invalidDistance(component.DistanceM) {
				return fmt.Errorf("contraction chain %d cost component is invalid", chainID)
			}
			componentDistance += component.DistanceM
		}
		if math.Abs(componentDistance-distance) > math.Max(1, distance)*1e-12 {
			return fmt.Errorf("contraction chain %d cost components do not match distance", chainID)
		}
	}
	for id := range g.Nodes {
		if !index.Retained[id] && len(index.Positions[id]) == 0 {
			return fmt.Errorf("contracted node %d has no chain position", id)
		}
	}
	return nil
}

func hasContractionPosition(positions []ContractionPosition, chain, offset int) bool {
	for _, position := range positions {
		if position.Chain == chain && position.Offset == offset {
			return true
		}
	}
	return false
}

func invalidDistance(distance float64) bool {
	return distance < 0 || math.IsNaN(distance) || math.IsInf(distance, 0)
}

func matchesBaseEdge(edges []Edge, want Edge) bool {
	for _, edge := range edges {
		if edge.To == want.To && edge.Cost == want.Cost && edge.DistanceM == want.DistanceM &&
			edge.Highway == want.Highway && edge.Name == want.Name && sameRestrictedModes(edge.RestrictedModes, want.RestrictedModes) {
			return true
		}
	}
	return false
}

func sameRestrictedModes(a, b []RestrictedMode) bool {
	if len(a) != len(b) {
		return false
	}
	a = append([]RestrictedMode(nil), a...)
	b = append([]RestrictedMode(nil), b...)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
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
