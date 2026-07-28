# PathCraft

**PathCraft is an extensible Go routing engine for maps, transit, simulations, and custom graphs.**

It ships working routing today: OSM street routes via A*, GTFS transit routes via RAPTOR, timetable-aware walk+transit journeys, a 3D air-routing reference, GeoJSON export, a CLI, an HTTP debug/demo server, and one compile-time registry for custom routing modes, loaders, algorithms, exporters, and cost models.

## Features

- **Walking routing**: parse OSM XML / `.osm.gz`, build a walkable graph, and route by node ID or coordinates.
- **Transit routing**: ingest GTFS `stop_times.txt`, `trips.txt`, optional `transfers.txt`, and optional `stops.txt`; query earliest arrivals with RAPTOR.
- **Time-dependent multimodal journeys**: compare direct walking against walk → scheduled transit → walk, with timed journey legs.
- **Plugin registry**: `core.Mode`, `core.Algorithm`, `core.GraphLoader`, `core.Exporter`, and `core.CostModel` extension points under `pkg/plugins`.
- **Engine configuration**: validated primitive walking speed and per-highway penalties; routing-mode policy lives in plugins.
- **Multiple surfaces**: Go SDK, CLI, HTTP, protobuf/gRPC, browser WASM, and a Leaflet routing demo.
- **Opt-in CORS**: exact browser origins, disabled by default.
- **GeoJSON output**: route and graph visualization through FeatureCollections.
- **Scale foundations**: exact directed degree-two contraction, source-safe atomic graph caches, and concurrent read-only routing.
- **Tests and benchmarks**: coverage for graph, OSM, GTFS, A*, RAPTOR, HTTP, engine, registry, plugin pipelines, allocations, and parallel queries.

## Prerequisites

- Go `1.25.12` (see `go.mod`)
- `make`
- `curl` only if you use `make fetch-osm`
- Node.js 24 only for frontend and WASM smoke-test development

Go dependencies are pinned in `go.mod` and `go.sum`.

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
| `zap`     | logger    | Development structured logger     |
| `walk` / `bike` / `car` | mode | OSM street-routing policies |
| `gtfs`    | mode      | Timetable-aware multimodal routing |
| `air`     | mode      | N-dimensional direct air route example |

### Run the plugin pipeline

Walking A* on OSM, exported as GeoJSON:

```bash
./bin/pathcraft pipeline \
  --loader osm --graph testdata/example.osm \
  --algorithm astar --from 1 --to 6 \
  --export geojson
```

Transit RAPTOR on GTFS:

```bash
./bin/pathcraft pipeline \
  --loader gtfs --graph testdata/mini_gtfs \
  --algorithm raptor --from START_STOP --to END_STOP \
  --opt departure_time=05:00:00
```

### Classic commands

```bash
./bin/pathcraft parse --file testdata/example.osm
./bin/pathcraft preprocess --file testdata/example.osm
./bin/pathcraft route --file testdata/example.osm --from 1 --to 6 --coords
./bin/pathcraft route --file testdata/example.osm \
  --from-lat -8.05428 --from-lon -34.88130 \
  --to-lat -8.05480 --to-lon -34.88030 --coords
./bin/pathcraft transit --gtfs testdata/mini_gtfs --from START_STOP --to END_STOP --time 05:00:00
./bin/pathcraft journey --file testdata/example.osm --gtfs testdata/mini_gtfs \
  --from-lat -8.05428 --from-lon -34.88130 \
  --to-lat -8.05480 --to-lon -34.88030 --time 05:00:00
./bin/pathcraft serve --file testdata/example.osm --gtfs testdata/mini_gtfs --addr :8080
./bin/pathcraft serve --file testdata/example.osm --addr :8080 \
  --cors-origin https://app.example
./bin/pathcraft grpc --file testdata/example.osm --addr 127.0.0.1:9090

# Self-contained 3D plugin; no street graph required.
./bin/pathcraft route --mode air \
  --from-position '12.5648,55.6726,100' \
  --to-position '12.5717,55.6833,200' \
  --opt cruise_altitude_m=1500 --coords
```

`pathcraft grpc` is plaintext and unauthenticated. It binds loopback by default; add trusted TLS/auth infrastructure before any untrusted-network exposure.

## Go Library Quickstart

Use `pkg/pathcraft/engine` for the public orchestration API. The registry-backed pipeline lets callers choose which plugins to activate with blank imports.

```go
package main

import (
    "context"
    "fmt"

    "github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
    pcengine "github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"

    _ "github.com/danielscoffee/pathcraft/pkg/plugins/astar"
    _ "github.com/danielscoffee/pathcraft/pkg/plugins/geojson"
    _ "github.com/danielscoffee/pathcraft/pkg/plugins/osm"
)

func main() {
    res, err := pcengine.Run(context.Background(), pcengine.PipelineRequest{
        LoaderName:    "osm",
        Source:        "testdata/example.osm",
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

The `engine.Engine` facade supplies data loading and primitive street/transit operations to plugins. `engine.New()` keeps a walking primitive default; `engine.NewWithConfig(engine.Config{...})` validates primitive speed and per-highway penalty multipliers. Applications select discoverable routing behavior through `core.Mode` plugins.

Full guide: [Go SDK](docs/sdk/go.md).

## Ecosystem APIs

- [JavaScript/WASM SDK](docs/sdk/javascript.md) — synchronous in-browser street routing from plain OSM XML.
- [gRPC API](docs/api/grpc.md) — generated `pathcraft.v1` client/server contract for street routes and multimodal journeys.
- [Plugin system](docs/architecture/plugin-system.md) — compile-time routing modes, algorithms, loaders, exporters, cost models, and loggers.

These interfaces remain pre-release. WASM does not load GTFS; gRPC has no built-in TLS or authentication.

## Interactive Demo

Run the Leaflet demo, then click two points on the map. The UI discovers registered modes compatible with its geographic renderer, builds controls from manifest options, resolves modes independently, and draws plugin-supplied segments. Leaflet projects longitude/latitude; 3D altitude remains present in air-route API results.

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

The HTTP server is a GET-only debug/demo interface, not a stable production API. CORS is disabled by default; `--cors-origin` accepts comma-separated exact HTTP(S) origins and enables browser preflight without credentials. Wildcard, opaque, and path-bearing origins are ignored.

- `GET /health`, `GET /status`
- `GET /graph` — loaded walking graph as GeoJSON lines
- `GET /nodes?bbox=minLon,minLat,maxLon,maxLat&limit=200&min_degree=3` — graph nodes as GeoJSON points
- `GET /nearest?lat=...&lon=...` — nearest graph node and snap distance
- `GET /modes` — manifests for every registered routing mode
- `GET /mode-route?mode=air&from=lon,lat,alt&to=lon,lat,alt` — generic N-dimensional plugin routing
- `GET /route?from=<id>&to=<id>` — node-id walking route as GeoJSON
- `GET /route?from_lat=...&from_lon=...&to_lat=...&to_lon=...` — coordinate walking route as GeoJSON
- `GET /journey?from_lat=...&from_lon=...&to_lat=...&to_lon=...&time=HH:MM:SS` — timetable-aware walk+transit journey JSON
- `GET /transit/stops`, `GET /transit/trips`, `GET /transit/trip?trip_id=...`
- `GET /graph-visual` — Leaflet viewer

## Architecture

```text
api/pathcraft/v1          protobuf contract and generated Go client/server types
cmd/pathcraft             CLI entrypoint
cmd/pathcraft-wasm        browser WebAssembly entrypoint
pkg/pathcraft/core        dimension-neutral plugin interfaces and value types
pkg/pathcraft/engine      public engine facade and pipeline runner
pkg/plugins               registry, standard plugins, and nearest-node index surface
internal/graph            private graph, contraction index, and cache format
internal/osm              OSM parser and graph builder
internal/gtfs             GTFS parsers and RAPTOR-ready indexes
internal/routing          A* and RAPTOR implementations
internal/http             HTTP demo/debug adapter
internal/grpcapi          protobuf/gRPC adapter
internal/wasmapi          JSON bridge for JavaScript/WASM
sdk/js                    JavaScript WebAssembly loader
web/app                   embedded React/Leaflet app
testdata                  tracked small OSM and GTFS fixtures
```

See also:

- [Documentation index](docs/README.md)
- [Architecture overview](docs/architecture/overview.md)
- [Current system audit](docs/architecture/current-system.md)
- [Plugin system](docs/architecture/plugin-system.md)
- [Go SDK](docs/sdk/go.md)
- [JavaScript/WASM SDK](docs/sdk/javascript.md)
- [gRPC API](docs/api/grpc.md)
- [Roadmap](docs/ROADMAP.md)

## Configuration

- `engine.Config` sets primitive walking speed in m/s and OSM-highway penalty multipliers (`>= 1`); registered mode plugins own mode defaults/policy.
- CLI flags configure OSM file paths, GTFS directories, node/coordinate endpoints, departure time, walking speed, server address, and optional exact CORS origins.
- `Makefile` variables:
  - `OSM_FILE` (default `testdata/example.osm`)
  - `GTFS_DIR` (default `testdata/mini_gtfs`)
  - `ADDR` (default `:8080`)
  - `BBOX` / `OUT` for `make fetch-osm`
- Parsed graph caches are written as `<osm-file>.cache`; source SHA-256 plus graph/preprocessing versions prevent stale reuse, writes replace atomically, and `*.cache` is ignored by git. Caches are trusted local build artifacts, not upload/network input; gob decoding precedes structural validation.

## Development

```bash
make test      # go test ./... -v -cover
make build     # build ./bin/pathcraft
make clean     # remove ./bin/pathcraft
go test ./...  # faster local test run without verbose coverage
```

Benchmarks live next to their packages and use standard Go tooling:

```bash
go test ./internal/routing/astar -bench=. -benchmem
go test ./pkg/pathcraft/engine -run '^$' -bench='(Preprocess|Parallel)' -benchmem
```

Contraction results and memory-profile workflow: [docs/performance.md](docs/performance.md).

## Project Status

PathCraft is a prototype routing engine. Phase 0.1–1.0 library, HTTP, timetable-routing, scale-foundation, SDK, WASM, gRPC, and plugin deliverables work at documented scope, but production hardening remains. Degree-two contraction is not full contraction hierarchies; base graph remains resident, loading/hot reload is not concurrent, gRPC is local plaintext by default, and WASM street routing is synchronous. Known next steps include GTFS service calendars/realtime, richer stop access, city-scale profiling budgets, API security, and deployment packaging.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Keep changes deterministic, formatted, tested, and aligned with the internal/public package boundaries.

## License

[Apache 2.0](LICENSE)
