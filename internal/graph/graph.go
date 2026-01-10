package graph

import (
	"encoding/gob"
	"os"

	"github.com/danielscoffee/pathcraft/internal/time"
)

type NodeID int64

type Edge struct {
	To        NodeID
	Cost      time.Seconds
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

// NearestNode returns the ID of the node closest to the given coordinates.
// WARN: This is a linear search and should be optimized with a spatial index for large graphs.
func (g *Graph) NearestNode(lat, lon float64, distanceFunc func(lat1, lon1, lat2, lon2 float64) float64) (NodeID, float64) {
	var nearest NodeID
	minDist := -1.0

	for id, node := range g.Nodes {
		dist := distanceFunc(lat, lon, node.Lat, node.Lon)
		if minDist < 0 || dist < minDist {
			minDist = dist
			nearest = id
		}
	}

	return nearest, minDist
}

// Save serializes the graph to a file.
func (g *Graph) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return gob.NewEncoder(f).Encode(g)
}

// LoadGraph deserializes a graph from a file.
func LoadGraph(path string) (*Graph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var g Graph
	if err := gob.NewDecoder(f).Decode(&g); err != nil {
		return nil, err
	}
	return &g, nil
}

func (g *Graph) HasNode(id NodeID) bool {
	_, ok := g.Nodes[id]
	return ok
}
