package plugins

import "testing"

func TestGridNearestNodeIndexIncludesAdjacentCellCandidates(t *testing.T) {
	idx := NewGridNearestNodeIndex()
	idx.Insert(IndexedNode{ID: 1, Lat: 0.0010, Lon: 0.0010}) // same cell, farther from query
	idx.Insert(IndexedNode{ID: 2, Lat: 0.0021, Lon: 0.0001}) // adjacent cell, closer to query

	candidates := idx.NearestCandidates(0.00199, 0.0001)

	if !containsCandidate(candidates, 2) {
		t.Fatalf("expected candidates to include closer adjacent-cell node 2, got %v", candidates)
	}
}

func containsCandidate(candidates []int64, id int64) bool {
	for _, candidate := range candidates {
		if candidate == id {
			return true
		}
	}
	return false
}
