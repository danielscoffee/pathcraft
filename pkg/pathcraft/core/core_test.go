package core

import (
	"context"
	"testing"
)

// staticGraph is a tiny in-memory Graph used to verify the interfaces compile
// and behave as plain Go values.
type staticGraph struct {
	edges map[NodeID][]Edge
}

func (s *staticGraph) Neighbors(_ context.Context, n NodeID) ([]Edge, error) {
	return s.edges[n], nil
}

func TestGraphInterfaceSatisfied(t *testing.T) {
	var g Graph = &staticGraph{edges: map[NodeID][]Edge{
		"a": {{From: "a", To: "b", Cost: 1}},
	}}
	got, err := g.Neighbors(context.Background(), "a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].To != "b" {
		t.Fatalf("unexpected neighbors: %+v", got)
	}
}

func TestRouteResultZeroValue(t *testing.T) {
	var r RouteResult
	if r.Path != nil || r.Cost != 0 || r.DurationMS != 0 {
		t.Fatalf("zero RouteResult should be empty: %+v", r)
	}
}
