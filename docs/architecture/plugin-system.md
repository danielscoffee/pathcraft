# Plugin System

PathCraft uses one compile-time registry in `pkg/plugins`. Plugins are ordinary Go packages implementing contracts from `pkg/pathcraft/core`; importing a package activates its `init()` registration. There is no dynamic `.so` loading.

## Product rule

Core owns generic graph, route, and plugin contracts. Optional routing behavior belongs to plugins. Adapters discover registered capabilities and translate them for CLI, HTTP, gRPC, WASM, or visual clients.

A plugin must not need changes to core merely because its domain is road, rail, sea, air, indoor, simulated, or orbital routing.

## Registered capabilities

| Interface | Responsibility |
|---|---|
| `core.Graph` | Generic node/edge access |
| `core.Algorithm` | Route over a graph |
| `core.GraphLoader` | Load a graph source |
| `core.Exporter` | Encode a route result |
| `core.CostModel` | Reweight or exclude edges |
| `core.LoggerPlugin` | Supply logging implementation |
| `core.Mode` | High-level discoverable routing experience |

`pkg/plugins` also contains the public nearest-node-index surface used by the internal graph.

## Routing modes

A mode advertises a manifest and accepts N-dimensional positions:

```go
type Mode interface {
    Name() string
    Manifest() ModeManifest
    Route(context.Context, any, ModeRequest) (ModeResult, error)
}
```

`ModeManifest` declares ID, label, icon key, color, CRS, supported dimensions, axis names, and options. `ModeRequest` contains plugin-defined `From`/`To` positions plus string options. `ModeResult` contains generic styled route segments whose positions retain every dimension.

Core deliberately treats `host` as opaque. A built-in street mode asserts the minimal engine capability it needs; a self-contained mode such as `air` ignores host; a custom application may pass its own runtime. This escape hatch prevents core from accumulating OSM, GTFS, aircraft, spacecraft, HTTP, or rendering dependencies.

## Registry

```go
import "github.com/danielscoffee/pathcraft/pkg/plugins"

registry := plugins.New() // isolated registry
registry.RegisterMode(myMode)

plugins.MustRegisterMode(myMode) // process-wide plugins.Default
```

Every capability supports registration, lookup, duplicate rejection, and sorted name discovery. `plugins.Default` is the process-wide registry used by plugin `init()` functions and `pathcraft plugins list`.

## Custom mode example

```go
package space

import (
    "context"

    "github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
    "github.com/danielscoffee/pathcraft/pkg/plugins"
)

type Plugin struct{}

func (Plugin) Name() string { return "space" }
func (Plugin) Manifest() core.ModeManifest {
    return core.ModeManifest{
        ID:         "space",
        Label:      "Space",
        Icon:       "space",
        Color:      "#111827",
        CRS:        "spacecraft-local",
        Dimensions: []int{3},
        Axes:       []string{"x", "y", "z"},
    }
}
func (Plugin) Route(_ context.Context, _ any, req core.ModeRequest) (core.ModeResult, error) {
    return core.ModeResult{
        Mode: "space",
        Segments: []core.RouteSegment{{
            Kind: "space", Positions: []core.Position{req.From, req.To},
        }},
    }, nil
}

func init() { plugins.MustRegisterMode(Plugin{}) }
```

Activate it in an executable:

```go
import _ "example.com/my-routing/space"
```

The registry-backed HTTP adapter then exposes it from `GET /modes` and dispatches it through `GET /mode-route?mode=space&from=1,2,3&to=4,5,6`. A visual client may render it when it supports the manifest's CRS and dimensions.

## Built-in plugins

| Names | Kind |
|---|---|
| `osm`, `gtfs` | loaders |
| `astar`, `raptor` | algorithms |
| `geojson` | exporter |
| `zap` | logger |
| `walk`, `bike`, `car`, `gtfs` | routing modes backed by `engine.Engine` |
| `air` | self-contained 2D/3D reference routing mode |

Built-ins live under `pkg/plugins/<name>`. `air` proves route contracts preserve altitude; it is a direct-route example, not a production flight planner or restricted-airspace model.

## Native capability escape hatches

Bundled OSM and GTFS graph adapters expose narrow `Native` interfaces so A* and RAPTOR wrappers can reuse optimized internal data. External graph/algorithm plugins can remain pure `core.Graph` implementations.

## Non-goals

- Runtime Go `.so` loading
- Remote plugin execution
- Hot reload
- Plugin marketplace or trust sandbox

Those can be layered later without changing the compile-time registry.
