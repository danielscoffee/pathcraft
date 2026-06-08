package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
)

type fakeAlgo struct{ name string }

func (f fakeAlgo) Name() string { return f.name }
func (f fakeAlgo) Route(_ context.Context, _ core.Graph, _ core.RouteRequest) (core.RouteResult, error) {
	return core.RouteResult{}, nil
}

func TestRegisterAndLookup(t *testing.T) {
	r := New()
	if err := r.RegisterAlgorithm(fakeAlgo{name: "x"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, ok := r.Algorithm("x"); !ok {
		t.Fatalf("expected algorithm x to be registered")
	}
	if _, ok := r.Algorithm("missing"); ok {
		t.Fatalf("unexpected lookup hit")
	}
}

func TestDuplicateRejected(t *testing.T) {
	r := New()
	_ = r.RegisterAlgorithm(fakeAlgo{name: "x"})
	err := r.RegisterAlgorithm(fakeAlgo{name: "x"})
	var dup ErrDuplicate
	if !errors.As(err, &dup) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestAlgorithmsSorted(t *testing.T) {
	r := New()
	_ = r.RegisterAlgorithm(fakeAlgo{name: "b"})
	_ = r.RegisterAlgorithm(fakeAlgo{name: "a"})
	got := r.Algorithms()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("expected sorted [a b], got %v", got)
	}
}
