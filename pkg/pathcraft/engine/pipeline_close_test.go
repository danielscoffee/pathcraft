package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

type closeTestGraph struct{ closed bool }

func (graph *closeTestGraph) Neighbors(context.Context, core.NodeID) ([]core.Edge, error) {
	return nil, nil
}

func (graph *closeTestGraph) Close() error {
	graph.closed = true
	return nil
}

type closeTestLoader struct{ graph *closeTestGraph }

func (loader closeTestLoader) Name() string { return "close-test" }
func (loader closeTestLoader) Load(context.Context, string) (core.Graph, error) {
	return loader.graph, nil
}

type closeTestAlgorithm struct{ fail bool }

func (algorithm closeTestAlgorithm) Name() string { return "close-test" }
func (algorithm closeTestAlgorithm) Route(context.Context, core.Graph, core.RouteRequest) (core.RouteResult, error) {
	if algorithm.fail {
		return core.RouteResult{}, errors.New("route failed")
	}
	return core.RouteResult{}, nil
}

func TestRunClosesLoadedGraphOnFailure(t *testing.T) {
	graph := &closeTestGraph{}
	registry := plugins.New()
	if err := registry.RegisterLoader(closeTestLoader{graph: graph}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterAlgorithm(closeTestAlgorithm{fail: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), PipelineRequest{
		LoaderName: "close-test", AlgorithmName: "close-test", Registry: registry,
	}); err == nil {
		t.Fatal("Run() error = nil")
	}
	if !graph.closed {
		t.Fatal("Run() did not close graph after failure")
	}
}

func TestPipelineResultTransfersGraphOwnership(t *testing.T) {
	graph := &closeTestGraph{}
	registry := plugins.New()
	if err := registry.RegisterLoader(closeTestLoader{graph: graph}); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterAlgorithm(closeTestAlgorithm{}); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), PipelineRequest{
		LoaderName: "close-test", AlgorithmName: "close-test", Registry: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	if graph.closed {
		t.Fatal("Run() closed successful result before ownership transfer")
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
	if !graph.closed {
		t.Fatal("PipelineResult.Close() did not close graph")
	}
}
