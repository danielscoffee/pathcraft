package graph

import (
	"encoding/gob"
	"fmt"
	"os"

	"github.com/danielscoffee/pathcraft/internal/time"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

type NodeID int64

type RestrictedMode string

const (
	RestrictedDriving RestrictedMode = "driving"
	RestrictedWalking RestrictedMode = "walking"
)

type Edge struct {
	To              NodeID
	Cost            time.Seconds
	DistanceM       float64
	Highway         string
	Name            string
	RestrictedModes []RestrictedMode
}

type Node struct {
	ID  NodeID
	Lat float64
	Lon float64
}

const CacheVersion = 3

type Graph struct {
	CacheVersion int
	Nodes        map[NodeID]Node
	Edges        map[NodeID][]Edge
	Contraction  *ContractionIndex

	nearestNodeIndex plugins.NearestNodeIndex
}

func NewGraph() *Graph {
	return &Graph{
		CacheVersion:     CacheVersion,
		Nodes:            make(map[NodeID]Node),
		Edges:            make(map[NodeID][]Edge),
		nearestNodeIndex: plugins.NewGridNearestNodeIndex(),
	}
}

func (g *Graph) AddNode(id NodeID, lat, lon float64) {
	g.Contraction = nil
	g.Nodes[id] = Node{ID: id, Lat: lat, Lon: lon}
	if g.nearestNodeIndex != nil {
		g.nearestNodeIndex.Insert(toIndexedNode(g.Nodes[id]))
	}
}

func (g *Graph) AddEdge(from, to NodeID, distanceM float64) {
	g.AddEdgeWithMeta(from, to, distanceM, "", "")
}

func (g *Graph) AddBidirectionalEdge(a, b NodeID, distanceM float64) {
	g.AddEdge(a, b, distanceM)
	g.AddEdge(b, a, distanceM)
}

func (g *Graph) AddEdgeWithMeta(from, to NodeID, distanceM float64, highway, name string) {
	g.AddRestrictedEdgeWithMeta(from, to, distanceM, highway, name)
}

func (g *Graph) AddRestrictedEdgeWithMeta(from, to NodeID, distanceM float64, highway, name string, restrictedModes ...RestrictedMode) {
	g.Contraction = nil
	g.Edges[from] = append(g.Edges[from], Edge{
		To:              to,
		DistanceM:       distanceM,
		Highway:         highway,
		Name:            name,
		RestrictedModes: append([]RestrictedMode(nil), restrictedModes...),
	})
}

func (g *Graph) Neighbors(id NodeID) []Edge {
	return g.Edges[id]
}

// NearestNode returns the ID of the node closest to the given coordinates.
func (g *Graph) NearestNode(lat, lon float64, distanceFunc func(lat1, lon1, lat2, lon2 float64) float64) (NodeID, float64) {
	g.ensureNearestNodeIndex()

	var nearest NodeID
	minDist := -1.0
	visited := false

	for _, id := range g.nearestNodeIndex.NearestCandidates(lat, lon) {
		node := g.Nodes[NodeID(id)]
		dist := distanceFunc(lat, lon, node.Lat, node.Lon)
		if minDist < 0 || dist < minDist {
			minDist = dist
			nearest = NodeID(id)
		}
		visited = true
	}

	if visited {
		return nearest, minDist
	}

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
	if g.CacheVersion != CacheVersion {
		return nil, fmt.Errorf("unsupported graph cache version %d, want %d", g.CacheVersion, CacheVersion)
	}
	g.rebuildNearestNodeIndex()
	return &g, nil
}

func (g *Graph) HasNode(id NodeID) bool {
	_, ok := g.Nodes[id]
	return ok
}

func (g *Graph) SetNearestNodeIndex(index plugins.NearestNodeIndex) {
	g.nearestNodeIndex = index
	g.rebuildNearestNodeIndex()
}

func (g *Graph) ensureNearestNodeIndex() {
	if g.nearestNodeIndex != nil {
		return
	}
	g.nearestNodeIndex = plugins.NewGridNearestNodeIndex()
	g.rebuildNearestNodeIndex()
}

func (g *Graph) rebuildNearestNodeIndex() {
	if g.nearestNodeIndex == nil {
		return
	}

	nodes := make([]plugins.IndexedNode, 0, len(g.Nodes))
	for _, node := range g.Nodes {
		nodes = append(nodes, toIndexedNode(node))
	}
	g.nearestNodeIndex.Rebuild(nodes)
}

func toIndexedNode(node Node) plugins.IndexedNode {
	return plugins.IndexedNode{
		ID:  int64(node.ID),
		Lat: node.Lat,
		Lon: node.Lon,
	}
}
