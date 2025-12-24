package graph

import t "github.com/qoppatech/exp-pathcraft/internal/domain/time"

type NodeID int64

type Edge struct {
	To        NodeID
	Cost      t.Seconds
	DistanceM float64
}

type Node struct {
	ID  NodeID
	Lat float64
	Lon float64
}

type Graph struct {
	Nodes map[NodeID]Node
	Edges map[NodeID][]Edge
}

func NewGraph() *Graph {
	return &Graph{
		Nodes: make(map[NodeID]Node),
		Edges: make(map[NodeID][]Edge),
	}
}

func (g *Graph) AddNode(id NodeID, lat, lon float64) {
	g.Nodes[id] = Node{ID: id, Lat: lat, Lon: lon}
}

func (g *Graph) AddEdge(from, to NodeID, distanceM float64) {
	g.Edges[from] = append(g.Edges[from], Edge{
		To:        to,
		DistanceM: distanceM,
	})
}

func (g *Graph) AddBidirectionalEdge(a, b NodeID, distanceM float64) {
	g.AddEdge(a, b, distanceM)
	g.AddEdge(b, a, distanceM)
}

func (g *Graph) Neighbors(id NodeID) []Edge {
	return g.Edges[id]
}

func (g *Graph) HasNode(id NodeID) bool {
	_, ok := g.Nodes[id]
	return ok
}
