package graph

import (
	"encoding/gob"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

func TestNearestNodeUsesSpatialIndex(t *testing.T) {
	g := NewGraph()
	g.AddNode(1, -8.05428, -34.88130)
	g.AddNode(2, -8.05428, -34.88080)
	g.AddNode(3, -8.05480, -34.88030)

	id, dist := g.NearestNode(-8.05479, -34.88031, planarDistance)
	if id != 3 {
		t.Fatalf("nearest node = %d, want 3", id)
	}
	if dist <= 0 {
		t.Fatalf("distance = %f, want > 0", dist)
	}
}

func TestLoadGraphRejectsUnversionedCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.gob")

	old := &Graph{
		Nodes: map[NodeID]Node{1: {ID: 1, Lat: 0, Lon: 0}},
		Edges: map[NodeID][]Edge{},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := gob.NewEncoder(f).Encode(old); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := LoadGraph(path); err == nil {
		t.Fatal("expected unversioned graph cache to be rejected")
	}
}

func TestLoadGraphRebuildsSpatialIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.gob")

	original := NewGraph()
	original.AddNode(10, -8.05428, -34.88130)
	original.AddNode(20, -8.05480, -34.88030)
	if err := original.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := LoadGraph(path)
	if err != nil {
		t.Fatalf("LoadGraph() error = %v", err)
	}

	id, _ := loaded.NearestNode(-8.05479, -34.88031, planarDistance)
	if id != 20 {
		t.Fatalf("nearest node after load = %d, want 20", id)
	}
}

func TestSetNearestNodeIndexUsesCustomPlugin(t *testing.T) {
	g := NewGraph()
	g.AddNode(1, -8.05428, -34.88130)
	g.AddNode(2, -8.05480, -34.88030)

	g.SetNearestNodeIndex(&stubNearestNodeIndex{candidates: []int64{2}})

	id, _ := g.NearestNode(-8.05428, -34.88130, planarDistance)
	if id != 2 {
		t.Fatalf("nearest node = %d, want 2 from custom plugin", id)
	}
}

func planarDistance(lat1, lon1, lat2, lon2 float64) float64 {
	return math.Hypot(lat2-lat1, lon2-lon1)
}

type stubNearestNodeIndex struct {
	candidates []int64
}

func (s *stubNearestNodeIndex) Insert(node plugins.IndexedNode) {}

func (s *stubNearestNodeIndex) Rebuild(nodes []plugins.IndexedNode) {}

func (s *stubNearestNodeIndex) NearestCandidates(lat, lon float64) []int64 {
	return s.candidates
}
