// Package osm registers the "osm" GraphLoader that parses an OSM XML file
// (.osm or .osm.gz) and returns a core.Graph backed by the internal
// graph package.
package osm

import (
	"context"
	"fmt"

	iosm "github.com/danielscoffee/pathcraft/internal/osm"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/osmgraph"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/registry"
)

type Loader struct{}

func (Loader) Name() string { return "osm" }

func (Loader) Load(_ context.Context, source string) (core.Graph, error) {
	if source == "" {
		return nil, fmt.Errorf("osm loader: source path is required")
	}
	data, err := iosm.ParseFile(source)
	if err != nil {
		return nil, fmt.Errorf("osm loader: %w", err)
	}
	g := iosm.BuildGraph(data, nil)
	return osmgraph.Wrap(g), nil
}

func init() { registry.MustRegisterLoader(Loader{}) }
