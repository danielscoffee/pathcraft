# Go SDK

PathCraft's pre-release Go SDK lives in `pkg/pathcraft/engine`. It loads routing data, applies configuration, and exposes street, transit, and multimodal queries. APIs may still change before a tagged v1 release.

## Install

```bash
go get github.com/danielscoffee/pathcraft
```

Use the Go version declared in PathCraft's `go.mod` or newer.

## Street routing

Load OSM from a file and route through a registered mode:

```go
package main

import (
    "context"
    "fmt"

    "github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
    "github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
    "github.com/danielscoffee/pathcraft/pkg/plugins"
    _ "github.com/danielscoffee/pathcraft/pkg/plugins/bike"
)

func main() {
    router := engine.New()
    if err := router.LoadOSM("city.osm.gz"); err != nil {
        panic(err)
    }

    mode, ok := plugins.Default.Mode("bike")
    if !ok {
        panic("bike mode not registered")
    }
    route, err := mode.Route(context.Background(), router, core.ModeRequest{
        From: core.Position{-34.88130, -8.05428},
        To:   core.Position{-34.87970, -8.05520},
    })
    if err != nil {
        panic(err)
    }

    fmt.Printf("%.0f m in %d seconds\n", route.DistanceMeters, route.DurationSeconds)
}
```

`LoadOSM` accepts `.osm` and `.osm.gz` paths. Embedded callers can avoid temporary files with `LoadOSMReader(io.Reader)`; reader input must be plain OSM XML.

Node-ID routing uses `Route(engine.RouteRequest{From: ..., To: ...})`. `RouteGeoJSON` and `RouteGeoJSONByCoordinates` return GeoJSON bytes.

### Configuration

`engine.Config` controls low-level street primitives:

- `SpeedMPS`: positive default walking speed; zero uses `1.4` m/s;
- `HighwayPenalties`: multipliers of at least `1` keyed by OSM highway type.

Routing-mode defaults belong to registered mode plugins. Invalid speeds and penalties fail in `NewWithConfig`.

## Versioned regional world graphs

Stream an OSM PBF into a local generation store, then pass its router to
existing street modes:

```go
ctx := context.Background()
manifest, err := builder.Build(ctx, builder.Options{
    PBFPath:   "region.osm.pbf",
    StorePath: "world",
    Region:    "demo",
    Zoom:      12,
})
if err != nil {
    panic(err)
}

router, err := worldgraph.OpenRouter("world", worldgraph.RouterOptions{})
if err != nil {
    panic(err)
}
defer router.Close()

mode, ok := plugins.Default.Mode("car") // blank-import pkg/plugins/car
if !ok {
    panic("car mode not registered")
}
route, err := mode.Route(ctx, router, core.ModeRequest{
    From: core.Position{12.5683, 55.6761},
    To:   core.Position{12.5685, 55.6762},
})
if err != nil {
    panic(err)
}
fmt.Println(manifest.Generation, route.DistanceMeters)
```

Imports are `pkg/plugins/worldgraph`, `pkg/plugins/worldgraph/builder`, and the
desired mode package. `worldgraph.Loader` also registers as the `worldgraph`
`core.GraphLoader`; transparent coordinate routing through `Router` is the
intended bounded path.

Defaults: zoom 12, Web Mercator latitude `±85.05112878°`, one-tile halo, 256
route tiles, three expansions, and 512 MiB decoded cache. Override
`RouterOptions` for measured local needs. Stores use checksummed immutable
generations and atomic manifest replacement; open routers remain pinned while
same-name region replacement removes stale provenance. Missing coverage/files,
corruption, area caps, no path, and cancellation return errors without
partial/direct fallback. Chunk stores are trusted local artifacts, not upload
payloads; retain OpenStreetMap attribution. This is local/regional routing,
not a planet-scale hierarchy.

## Transit and multimodal routing

Load a GTFS directory beside the street graph:

```go
router := engine.New()
if err := router.LoadOSM("city.osm"); err != nil {
    panic(err)
}
if err := router.LoadGTFSDir("city-gtfs"); err != nil {
    panic(err)
}

journey, err := router.MultimodalRoute(engine.MultimodalRouteRequest{
    FromLat:       -8.05428,
    FromLon:       -34.88130,
    ToLat:         -8.05520,
    ToLon:         -34.87970,
    DepartureTime: "08:00:00",
})
if err != nil {
    panic(err)
}
```

GTFS time strings use `HH:MM:SS` and may exceed 24 hours where feeds do. `TransitRoute` routes between stop IDs. `GTFSStops`, `GTFSTripIDs`, and `GTFSTripStopTimes` expose loaded feed metadata.

Current limitation: service calendars are not applied, so loaded trips are treated as running.

## Preprocessed graph caches

`LoadOSM` parses, builds, contracts, and publishes an immutable graph. To persist it:

```go
if err := router.SaveGraph("city.osm.cache"); err != nil {
    panic(err)
}

cached := engine.New()
if err := cached.LoadGraph("city.osm.cache"); err != nil {
    panic(err)
}
```

Caches are Go-specific local artifacts. Format and preprocessing versions plus source SHA-256 reject stale or incompatible files. Do not accept untrusted cache uploads.

## Concurrency

After all OSM and GTFS loading finishes, one `Engine` supports concurrent read-only route calls. Do not load data, replace nearest-node indexes, mutate graphs, or hot-reload while queries run.

```go
var group sync.WaitGroup
for _, request := range requests {
    group.Add(1)
    go func() {
        defer group.Done()
        if _, err := router.RouteByCoordinates(request); err != nil {
            // Handle this request's error.
        }
    }()
}
group.Wait()
```

No worker pool is required; Go's scheduler handles independent calls.

## Registry pipeline and plugins

For loader → algorithm → exporter selection, blank-import desired built-ins and call `engine.Run`:

```go
import (
    "context"

    "github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
    "github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"

    _ "github.com/danielscoffee/pathcraft/pkg/plugins/astar"
    _ "github.com/danielscoffee/pathcraft/pkg/plugins/geojson"
    _ "github.com/danielscoffee/pathcraft/pkg/plugins/osm"
)

result, err := engine.Run(context.Background(), engine.PipelineRequest{
    LoaderName:    "osm",
    Source:        "city.osm",
    AlgorithmName: "astar",
    ExporterName:  "geojson",
    Route:         core.RouteRequest{From: "1", To: "6"},
})
if err != nil {
    panic(err)
}
defer result.Close() // successful calls transfer ownership of the loaded graph
```

Custom plugins are ordinary linked Go packages implementing interfaces from `pkg/pathcraft/core` and registering with `pkg/plugins`. High-level `core.Mode` plugins accept N-dimensional positions and return generic route segments, so custom domains do not require transport or UI changes. See [Plugin system](../architecture/plugin-system.md). PathCraft does not load plugins dynamically.

## API reference and runnable example

- Package reference: <https://pkg.go.dev/github.com/danielscoffee/pathcraft/pkg/pathcraft/engine>
- Runnable source: [`pkg/pathcraft/engine/example_test.go`](../../pkg/pathcraft/engine/example_test.go)
- Engine architecture: [Architecture overview](../architecture/overview.md)
