package engine

import (
	"context"
	"fmt"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/registry"
)

// PipelineRequest drives the load → solve → export pipeline that backs the
// `pathcraft route` CLI command and the public Run helper.
type PipelineRequest struct {
	LoaderName    string
	Source        string
	AlgorithmName string
	ExporterName  string
	Route         core.RouteRequest
	Registry      *registry.Registry
}

// PipelineResult bundles raw exporter output with the structured result.
type PipelineResult struct {
	Result   core.RouteResult
	Graph    core.Graph
	Output   []byte
	MimeType string
}

// Run executes loader → algorithm → exporter using req.Registry (or the
// process-wide registry.Default when nil).
func Run(ctx context.Context, req PipelineRequest) (PipelineResult, error) {
	reg := req.Registry
	if reg == nil {
		reg = registry.Default
	}

	loader, ok := reg.Loader(req.LoaderName)
	if !ok {
		return PipelineResult{}, fmt.Errorf("loader %q not registered", req.LoaderName)
	}
	g, err := loader.Load(ctx, req.Source)
	if err != nil {
		return PipelineResult{}, fmt.Errorf("loader %s: %w", req.LoaderName, err)
	}

	algo, ok := reg.Algorithm(req.AlgorithmName)
	if !ok {
		return PipelineResult{}, fmt.Errorf("algorithm %q not registered", req.AlgorithmName)
	}
	res, err := algo.Route(ctx, g, req.Route)
	if err != nil {
		return PipelineResult{}, fmt.Errorf("algorithm %s: %w", req.AlgorithmName, err)
	}

	out := PipelineResult{Result: res, Graph: g}
	if req.ExporterName == "" {
		return out, nil
	}
	exp, ok := reg.Exporter(req.ExporterName)
	if !ok {
		return out, fmt.Errorf("exporter %q not registered", req.ExporterName)
	}
	bytes, err := exp.Export(ctx, res, g)
	if err != nil {
		return out, fmt.Errorf("exporter %s: %w", req.ExporterName, err)
	}
	out.Output = bytes
	out.MimeType = exp.MimeType()
	return out, nil
}
