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
		return Path{
			Nodes:      []graph.NodeID{source},
			NodesCount: 1,
		}, nil
	}

	targetNode := g.Nodes[target]
	gScore := map[graph.NodeID]float64{source: 0}
	distanceScore := map[graph.NodeID]float64{source: 0}
	cameFrom := make(map[graph.NodeID]graph.NodeID)

	openSet := &priorityQueue{}
	heap.Init(openSet)
	heap.Push(openSet, &pqItem{
		nodeID:   source,
		priority: h(g.Nodes[source], targetNode),
	})

	for openSet.Len() > 0 {
		current := heap.Pop(openSet).(*pqItem)
		currentID := current.nodeID
		if current.cost != gScore[currentID] {
			continue
		}
		if currentID == target {
			return reconstructPath(cameFrom, target, gScore[target], distanceScore[target]), nil
		}

		for _, edge := range g.Neighbors(currentID) {
			if edgeRestrictedForProfile(edge, profile) {
				continue
			}
			cost, err := edgeCost(edge, profile)
			if err != nil {
				return Path{}, err
			}
			tentativeG := gScore[currentID] + cost
			existingG, visited := gScore[edge.To]
			if visited && tentativeG >= existingG {
				continue
			}

			cameFrom[edge.To] = currentID
			gScore[edge.To] = tentativeG
			distanceScore[edge.To] = distanceScore[currentID] + edge.DistanceM
			heap.Push(openSet, &pqItem{
				nodeID:   edge.To,
				cost:     tentativeG,
				priority: tentativeG + h(g.Nodes[edge.To], targetNode),
			})
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

func reconstructPath(cameFrom map[graph.NodeID]graph.NodeID, target graph.NodeID, totalCost, totalDistance float64) Path {
	path := []graph.NodeID{target}
	current := target

	for {
		prev, ok := cameFrom[current]
		if !ok {
			break
		}
		path = append(path, prev)
		current = prev
	}

	// Reverse to get source -> target order
	slices.Reverse(path)

	return Path{
		Nodes:         path,
		TotalCost:     totalCost,
		TotalDistance: totalDistance,
		NodesCount:    len(path),
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
