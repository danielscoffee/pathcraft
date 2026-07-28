# Plugin System

PathCraft uses a compile-time plugin registry, not dynamic loading. Plugins
are ordinary Go packages that satisfy interfaces from `pkg/pathcraft/core`
and register themselves with `pkg/pathcraft/registry.Default` in `init()`.

## Goals

- Make extension points visible: algorithms, graph loaders, exporters, cost models, loggers.
- Keep the runtime cost of "having a plugin system" at zero.
- Let library users pick their dependency tree by blank-importing only the
  plugins they need.
- Wrap existing `internal/*` engines instead of rewriting them.

## Non-goals (explicitly out of scope)

- Native Go `plugin` (.so) dynamic loading.
- WASM / gRPC / subprocess plugin runtimes.
- Hot-reload at runtime.
- A configuration file format for declaring which plugins to load.

If we ever need any of the above, we add it on top of the registry — the
registry stays simple.

## Interfaces

All declared in `pkg/pathcraft/core`:

| Interface     | Responsibility                                |
|---------------|-----------------------------------------------|
| `Graph`       | `Neighbors(ctx, NodeID) []Edge`               |
| `Algorithm`   | `Route(ctx, Graph, RouteRequest) RouteResult` |
| `GraphLoader` | `Load(ctx, source string) Graph`              |
| `Exporter`    | `Export(ctx, RouteResult, Graph) []byte`      |
| `CostModel`   | `Cost(Edge, RouteState) float64`              |
| `LoggerPlugin` | `Logger() (*zap.Logger, error)`              |

### Capability interfaces (optional)

A `Graph` may also implement `core.Coordinated` (nodes have lat/lon) or
`core.Sized` (knows its NodeCount). Algorithms type-assert for these and
fall back gracefully when absent.

### Native escape hatches

The built-in OSM and GTFS adapters expose narrow `Native` interfaces
(`osmgraph.Native`, `gtfsgraph.Native`) so the bundled `astar` and `raptor`
plugins can recover the original internal data structures. This avoids
reimplementing search over the generic `Graph.Neighbors` path for the
in-tree happy case while keeping the public API free of internal types.

External graphs that don't expose `Native` still work with any algorithm
written purely against `core.Graph`.

## Registry

```go
type Registry struct { /* algorithms, loaders, exporters, costs, loggers */ }

func New() *Registry           // isolated, for tests
var Default = New()            // process-wide, for plugin init()

func (r *Registry) RegisterAlgorithm(core.Algorithm) error
func (r *Registry) Algorithm(name string) (core.Algorithm, bool)
func (r *Registry) Algorithms() []string  // sorted
// ... same for Loader / Exporter / CostModel / Logger
```

`MustRegister*` panics on duplicate and is what plugin `init()` typically calls.

## Writing a plugin

A custom algorithm in three pieces:

```go
package myalgo

import (
    "context"
    "github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
    "github.com/danielscoffee/pathcraft/pkg/pathcraft/registry"
)

type Plugin struct{}
func (Plugin) Name() string { return "myalgo" }
func (Plugin) Route(ctx context.Context, g core.Graph, req core.RouteRequest) (core.RouteResult, error) {
    // your search here
    return core.RouteResult{Path: []core.NodeID{req.From, req.To}}, nil
}

func init() { registry.MustRegisterAlgorithm(Plugin{}) }
```

To activate it, blank-import the package from your `main`:

```go
import _ "yourmodule/myalgo"
```

## Built-in plugins

| Name      | Kind       | Wraps                        |
|-----------|------------|------------------------------|
| `osm`     | loader     | `internal/osm` + `internal/graph` |
| `gtfs`    | loader     | `internal/gtfs`              |
| `astar`   | algorithm  | `internal/routing/astar`     |
| `raptor`  | algorithm  | `internal/routing/raptor`    |
| `geojson` | exporter   | `internal/geojson`           |
| `zap`     | logger     | `go.uber.org/zap`            |

List them at runtime with `pathcraft plugins list`.
