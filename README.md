# PathCraft

**PathCraft is an extensible Go routing engine for maps, transit, simulations, and custom graphs.**

It ships working routing today: OSM walking routes via A*, GTFS transit routes via RAPTOR, initial walk+transit journeys, GeoJSON export, a CLI, an HTTP debug/demo server, and a compile-time plugin registry for custom loaders, algorithms, exporters, and cost models.

## Features

- **Walking routing**: parse OSM XML / `.osm.gz`, build a walkable graph, and route by node ID or coordinates.
- **Transit routing**: ingest GTFS `stop_times.txt`, `trips.txt`, optional `transfers.txt`, and optional `stops.txt`; query earliest arrivals with RAPTOR.
- **Initial multimodal journeys**: compare direct walking against walk → transit → walk using nearest GTFS stop candidates.
- **Plugin registry**: `core.Algorithm`, `core.GraphLoader`, `core.Exporter`, and `core.CostModel` extension points.
- **Multiple surfaces**: Go library, `pathcraft` CLI, HTTP endpoints, and a Leaflet routing demo.
- **GeoJSON output**: route and graph visualization through FeatureCollections.
- **Tests and benchmarks**: coverage for graph, OSM, GTFS, A*, RAPTOR, HTTP, engine, registry, and plugin pipeline behavior.

## Prerequisites

- Go `1.25.12` (see `go.mod`)
- `make`
- `curl` only if you use `make fetch-osm`

PathCraft currently has no third-party Go module dependencies.

## Install / Build

```bash
make build
./bin/pathcraft help
```

## CLI Quickstart

### List built-in plugins

```bash
./bin/pathcraft plugins list
```

Built-ins currently include:

| Name      | Kind      | Wraps / source                    |
|-----------|-----------|-----------------------------------|
| `osm`     | loader    | OSM XML / `.osm.gz` parser        |
| `gtfs`    | loader    | GTFS directory                    |
| `astar`   | algorithm | internal A* walking router        |
| `raptor`  | algorithm | internal RAPTOR transit router    |
| `geojson` | exporter  | GeoJSON `FeatureCollection`       |

### Run the plugin pipeline

Walking A* on OSM, exported as GeoJSON:

```bash
./bin/pathcraft pipeline \
  --loader osm --graph examples/example.osm \
  --algorithm astar --from 1 --to 6 \
  --export geojson
```

Transit RAPTOR on GTFS:

```bash
./bin/pathcraft pipeline \
  --loader gtfs --graph examples/mini_gtfs \
  --algorithm raptor --from START_STOP --to END_STOP \
  --opt departure_time=05:00:00
```

### Classic commands

```bash
./bin/pathcraft parse --file examples/example.osm
./bin/pathcraft route --file examples/example.osm --from 1 --to 6 --coords
./bin/pathcraft route --file examples/example.osm \
  --from-lat -8.05428 --from-lon -34.88130 \
  --to-lat -8.05480 --to-lon -34.88030 --coords
./bin/pathcraft transit --gtfs examples/gtfs --from RECIFE --to CAMARAGIBE --time 05:00:00
./bin/pathcraft journey --file examples/example.osm --gtfs examples/mini_gtfs \
  --from-lat -8.05428 --from-lon -34.88130 \
  --to-lat -8.05480 --to-lon -34.88030 --time 05:00:00
./bin/pathcraft serve --file examples/example.osm --gtfs examples/mini_gtfs --addr :8080
```

## Go Library Quickstart

Use `pkg/pathcraft/engine` for the public orchestration API. The registry-backed pipeline lets callers choose which plugins to activate with blank imports.

```go
package main

import (
    "context"
    "fmt"

    "github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
    pcengine "github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"

    _ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/astar"
    _ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/geojson"
    _ "github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/osm"
)

func main() {
    res, err := pcengine.Run(context.Background(), pcengine.PipelineRequest{
        LoaderName:    "osm",
        Source:        "examples/example.osm",
        AlgorithmName: "astar",
        ExporterName:  "geojson",
        Route:         core.RouteRequest{From: "1", To: "6"},
    })
    if err != nil {
        panic(err)
    }
    fmt.Printf("path nodes: %d, cost: %.1f\n", len(res.Result.Path), res.Result.Cost)
    fmt.Println(string(res.Output))
}
```

The older `engine.Engine` facade is also still available for direct OSM/GTFS loading, coordinate routing, transit routing, and multimodal journey search.

## Interactive Demo

Run the Leaflet demo, then click two points on the map. The server snaps each click to the nearest OSM node, runs A*, and draws the route.

```bash
make demo
# open http://localhost:8080/graph-visual
```

Use a real OSM extract for a richer map:

```bash
make fetch-osm BBOX="40.7480,-74.0050,40.7620,-73.9700" OUT=examples/manhattan.osm
make demo OSM_FILE=examples/manhattan.osm
```

Full guide: [docs/tutorials/demo.md](docs/tutorials/demo.md).

## HTTP Endpoints

The HTTP server is a debug/demo interface, not a stable production API.

- `GET /health`, `GET /status`
- `GET /graph` — loaded walking graph as GeoJSON lines
- `GET /nodes?bbox=minLon,minLat,maxLon,maxLat&limit=200&min_degree=3` — graph nodes as GeoJSON points
- `GET /nearest?lat=...&lon=...` — nearest graph node and snap distance
- `GET /route?from=<id>&to=<id>` — node-id walking route as GeoJSON
- `GET /route?from_lat=...&from_lon=...&to_lat=...&to_lon=...` — coordinate walking route as GeoJSON
- `GET /journey?from_lat=...&from_lon=...&to_lat=...&to_lon=...&time=HH:MM:SS` — initial walk+transit journey JSON
- `GET /transit/stops`, `GET /transit/trips`, `GET /transit/trip?trip_id=...`
- `GET /graph-visual` — Leaflet viewer

## Architecture

```text
cmd/pathcraft             CLI entrypoint
pkg/pathcraft/core        public plugin interfaces and value types
pkg/pathcraft/registry    compile-time plugin registry
pkg/pathcraft/engine      public engine facade and pipeline runner
pkg/pathcraft/plugins     built-in plugin adapters
pkg/plugins               nearest-node spatial index plugin surface
internal/graph            private graph model and cache format
internal/osm              OSM parser and graph builder
internal/gtfs             GTFS parsers and RAPTOR-ready indexes
internal/routing          A* and RAPTOR implementations
internal/http             HTTP demo/debug adapter
web/template              Leaflet demo
examples                  small OSM and GTFS fixtures
```

See also:

- [Documentation index](docs/README.md)
- [Architecture overview](docs/architecture/overview.md)
- [Current system audit](docs/architecture/current-system.md)
- [Plugin system](docs/architecture/plugin-system.md)
- [Roadmap](docs/ROADMAP.md)

## Configuration

- CLI flags configure OSM file paths, GTFS directories, node/coordinate endpoints, departure time, walking speed, output path, and server address.
- `Makefile` variables:
  - `OSM_FILE` (default `examples/recife_demo.osm`)
  - `GTFS_DIR` (default `examples/mini_gtfs`)
  - `ADDR` (default `:8080`)
  - `BBOX` / `OUT` for `make fetch-osm`
- Parsed graph caches are written as `<osm-file>.cache`; `*.cache` is ignored by git.

## Development

```bash
make test      # go test ./... -v -cover
make build     # build ./bin/pathcraft
make clean     # remove ./bin/pathcraft
go test ./...  # faster local test run without verbose coverage
```

Benchmarks live next to their packages and can be run with standard Go tooling, for example:

```bash
go test ./internal/routing/astar -bench=. -benchmem
```

## Project Status

PathCraft is a prototype routing engine. The core walking, transit, plugin, CLI, and demo flows work, but production hardening is still pending. Known next steps include richer multimodal modeling, better stop discovery, CORS/API polish, deployment packaging, preprocessing, caching, and scale-oriented performance work.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Keep changes deterministic, formatted, tested, and aligned with the internal/public package boundaries.

## License

[Apache 2.0](LICENSE)
