# Go SDK

PathCraft's pre-release Go SDK lives in `pkg/pathcraft/engine`. It loads routing data, applies configuration, and exposes street, transit, and multimodal queries. APIs may still change before a tagged v1 release.

## Install

```bash
go get github.com/danielscoffee/pathcraft
```

Use the Go version declared in PathCraft's `go.mod` or newer.

## Street routing

Load OSM from a file and route between coordinates:

```go
package main

import (
    "fmt"

    "github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func main() {
    router, err := engine.NewWithConfig(engine.Config{
        Mode:     engine.ModeBike,
        SpeedMPS: 4.5,
    })
    if err != nil {
        panic(err)
    }
    if err := router.LoadOSM("city.osm.gz"); err != nil {
        panic(err)
    }

    route, err := router.RouteByCoordinates(engine.CoordinateRouteRequest{
        FromLat:            -8.05428,
        FromLon:            -34.88130,
        ToLat:              -8.05520,
        ToLon:              -34.87970,
        IncludeCoordinates: true,
    })
    if err != nil {
        panic(err)
    }

    fmt.Printf("%.0f m in %s\n", route.Distance, route.Duration)
}
```

`LoadOSM` accepts `.osm` and `.osm.gz` paths. Embedded callers can avoid temporary files with `LoadOSMReader(io.Reader)`; reader input must be plain OSM XML.

Node-ID routing uses `Route(engine.RouteRequest{From: ..., To: ...})`. `RouteGeoJSON` and `RouteGeoJSONByCoordinates` return GeoJSON bytes.

### Configuration

`engine.Config` controls default street behavior:

- `Mode`: `engine.ModeWalk`, `engine.ModeBike`, or `engine.ModeCar`;
- `SpeedMPS`: positive route speed; zero chooses mode default;
- `HighwayPenalties`: multipliers of at least `1` keyed by OSM highway type.

Requests with no explicit profile use engine defaults. Invalid modes, speeds, and penalties fail in `NewWithConfig`.

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

    _ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/astar"
    _ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/geojson"
    _ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/osm"
)

result, err := engine.Run(context.Background(), engine.PipelineRequest{
    LoaderName:    "osm",
    Source:        "city.osm",
    AlgorithmName: "astar",
    ExporterName:  "geojson",
    Route:         core.RouteRequest{From: "1", To: "6"},
})
```

Custom plugins are ordinary linked Go packages implementing interfaces from `pkg/pathcraft/core` and registering with `pkg/pathcraft/registry`. See [Plugin system](../architecture/plugin-system.md). PathCraft does not load plugins dynamically.

## API reference and runnable example

- Package reference: <https://pkg.go.dev/github.com/danielscoffee/pathcraft/pkg/pathcraft/engine>
- Runnable source: [`pkg/pathcraft/engine/example_test.go`](../../pkg/pathcraft/engine/example_test.go)
- Engine architecture: [Architecture overview](../architecture/overview.md)
