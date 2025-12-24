package astar

import (
	"container/heap"
	"errors"

	. "github.com/qoppatech/exp-pathcraft/internal/geo"
	"github.com/qoppatech/exp-pathcraft/internal/graph"
)

var ErrNoPath = errors.New("no path found")

var ErrNodeNotFound = errors.New("node not found in graph")

type Path struct {
	Nodes      []graph.NodeID
	TotalCost  float64
	NodesCount int
}

func AStar(g *graph.Graph, source, target graph.NodeID, h Heuristic) (Path, error) {
	if !g.HasNode(source) || !g.HasNode(target) {
		return Path{}, ErrNodeNotFound
	}

	if source == target {
		return Path{
			Nodes:      []graph.NodeID{source},
			TotalCost:  0,
			NodesCount: 1,
		}, nil
	}

	targetNode := g.Nodes[target]

	gScore := make(map[graph.NodeID]float64)
	gScore[source] = 0

	cameFrom := make(map[graph.NodeID]graph.NodeID)

	openSet := &priorityQueue{}
	heap.Init(openSet)
	heap.Push(openSet, &pqItem{
		nodeID:   source,
		priority: h(g.Nodes[source], targetNode),
	})

	inOpenSet := make(map[graph.NodeID]bool)
	inOpenSet[source] = true

	for openSet.Len() > 0 {
		current := heap.Pop(openSet).(*pqItem)
		currentID := current.nodeID

		if currentID == target {
			return reconstructPath(cameFrom, target, gScore[target]), nil
		}

		delete(inOpenSet, currentID)

		for _, edge := range g.Neighbors(currentID) {
			tentativeG := gScore[currentID] + edge.DistanceM
			existingG, visited := gScore[edge.To]
			if !visited || tentativeG < existingG {
				cameFrom[edge.To] = currentID
				gScore[edge.To] = tentativeG

				fScore := tentativeG + h(g.Nodes[edge.To], targetNode)

				if !inOpenSet[edge.To] {
					heap.Push(openSet, &pqItem{
						nodeID:   edge.To,
						priority: fScore,
					})
					inOpenSet[edge.To] = true
				}
			}
		}
	}

	return Path{}, ErrNoPath
}

func reconstructPath(cameFrom map[graph.NodeID]graph.NodeID, target graph.NodeID, totalCost float64) Path {
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
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	return Path{
		Nodes:      path,
		TotalCost:  totalCost,
		NodesCount: len(path),
	}
}

// Priority queue implementation for A*
type pqItem struct {
	nodeID   graph.NodeID
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
