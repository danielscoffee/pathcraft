package astar

import (
	"container/heap"
	"errors"
	"math"
	"slices"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/mobility"
)

var ErrNoPath = errors.New("no path found")

var ErrNodeNotFound = errors.New("node not found in graph")

var ErrInvalidCost = errors.New("invalid edge cost")

type Path struct {
	Nodes         []graph.NodeID
	TotalCost     float64
	TotalDistance float64
	NodesCount    int
	ExpandedNodes int
}

type pathStep struct {
	From graph.NodeID
	Via  []graph.NodeID
}

func AStar(g *graph.Graph, source, target graph.NodeID, h geo.Heuristic) (Path, error) {
	path, err := AStarWithProfile(g, source, target, h, nil)
	if err != nil {
		return Path{}, err
	}
	return path, nil
}

func AStarWithProfile(g *graph.Graph, source, target graph.NodeID, h geo.Heuristic, profile mobility.Profile) (Path, error) {
	if !g.HasNode(source) || !g.HasNode(target) {
		return Path{}, ErrNodeNotFound
	}
	if source == target {
		return Path{Nodes: []graph.NodeID{source}, NodesCount: 1, ExpandedNodes: 1}, nil
	}
	if _, ok := profile.(highwayPenaltyProfile); ok {
		h = func(_, _ graph.Node) float64 { return 0 }
	}

	targetNode := g.Nodes[target]
	gScore := map[graph.NodeID]float64{source: 0}
	distanceScore := map[graph.NodeID]float64{source: 0}
	cameFrom := make(map[graph.NodeID]pathStep)

	openSet := &priorityQueue{}
	heap.Init(openSet)
	heap.Push(openSet, &pqItem{nodeID: source, priority: h(g.Nodes[source], targetNode)})
	expanded := 0

	for openSet.Len() > 0 {
		current := heap.Pop(openSet).(*pqItem)
		currentID := current.nodeID
		if current.cost != gScore[currentID] {
			continue
		}
		expanded++
		if currentID == target {
			return reconstructPath(cameFrom, target, gScore[target], distanceScore[target], expanded), nil
		}

		relax := func(to graph.NodeID, cost, distance float64, via []graph.NodeID) {
			tentativeG := gScore[currentID] + cost
			existingG, visited := gScore[to]
			if visited && tentativeG >= existingG {
				return
			}
			cameFrom[to] = pathStep{From: currentID, Via: via}
			gScore[to] = tentativeG
			distanceScore[to] = distanceScore[currentID] + distance
			heap.Push(openSet, &pqItem{
				nodeID:   to,
				cost:     tentativeG,
				priority: tentativeG + h(g.Nodes[to], targetNode),
			})
		}

		if index := g.Contraction; index != nil && index.Version == graph.ContractionVersion {
			for _, arc := range index.Outgoing(currentID, source, target) {
				chain := index.Chains[arc.Chain]
				cost, distance, blocked, err := contractionArcCost(chain, arc.From, arc.To, profile)
				if err != nil {
					return Path{}, err
				}
				if blocked {
					continue
				}
				relax(chain.Nodes[arc.To], cost, distance, chain.Nodes[arc.From+1:arc.To])
			}
			continue
		}

		for _, edge := range g.Neighbors(currentID) {
			if edgeRestrictedForProfile(edge, profile) {
				continue
			}
			cost, err := edgeCost(edge, profile)
			if err != nil {
				return Path{}, err
			}
			relax(edge.To, cost, edge.DistanceM, nil)
		}
	}

	return Path{}, ErrNoPath
}

type highwayPenaltyProfile interface {
	HighwayPenalty(highway string) float64
}

func edgeCost(edge graph.Edge, profile mobility.Profile) (float64, error) {
	cost := edge.DistanceM
	if profile, ok := profile.(highwayPenaltyProfile); ok {
		cost *= profile.HighwayPenalty(edge.Highway)
	}
	if cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
		return 0, ErrInvalidCost
	}
	return cost, nil
}

func edgeRestrictedForProfile(edge graph.Edge, profile mobility.Profile) bool {
	if profile == nil {
		return false
	}
	for _, mode := range edge.RestrictedModes {
		if string(mode) == profile.Name() {
			return true
		}
	}
	return false
}

func contractionArcCost(chain graph.ContractionChain, from, to int, profile mobility.Profile) (cost, distance float64, blocked bool, err error) {
	if from == 0 && to == len(chain.Nodes)-1 {
		if restrictedModesForProfile(chain.RestrictedModes, profile) {
			return 0, 0, true, nil
		}
		if penaltyProfile, ok := profile.(highwayPenaltyProfile); ok {
			for _, component := range chain.CostComponents {
				cost += component.DistanceM * penaltyProfile.HighwayPenalty(component.Highway)
			}
		} else {
			cost = chain.DistanceM
		}
		distance = chain.DistanceM
		if invalidCost(cost) {
			return 0, 0, false, ErrInvalidCost
		}
		return cost, distance, false, nil
	}

	for _, edge := range chain.Segments[from:to] {
		if edgeRestrictedForProfile(edge, profile) {
			return 0, 0, true, nil
		}
		edgeCost, edgeErr := edgeCost(edge, profile)
		if edgeErr != nil {
			return 0, 0, false, edgeErr
		}
		cost += edgeCost
		distance += edge.DistanceM
	}
	return cost, distance, false, nil
}

func restrictedModesForProfile(modes []graph.RestrictedMode, profile mobility.Profile) bool {
	if profile == nil {
		return false
	}
	for _, mode := range modes {
		if string(mode) == profile.Name() {
			return true
		}
	}
	return false
}

func invalidCost(cost float64) bool {
	return cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0)
}

func reconstructPath(cameFrom map[graph.NodeID]pathStep, target graph.NodeID, totalCost, totalDistance float64, expanded int) Path {
	path := []graph.NodeID{target}
	current := target

	for {
		step, ok := cameFrom[current]
		if !ok {
			break
		}
		for i := len(step.Via) - 1; i >= 0; i-- {
			path = append(path, step.Via[i])
		}
		path = append(path, step.From)
		current = step.From
	}
	slices.Reverse(path)

	return Path{
		Nodes:         path,
		TotalCost:     totalCost,
		TotalDistance: totalDistance,
		NodesCount:    len(path),
		ExpandedNodes: expanded,
	}
}

// Priority queue implementation for A*
type pqItem struct {
	nodeID   graph.NodeID
	cost     float64
	priority float64 // fScore = gScore + heuristic
	index    int
}

type priorityQueue []*pqItem

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	return pq[i].priority < pq[j].priority
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x any) {
	n := len(*pq)
	item := x.(*pqItem)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}
