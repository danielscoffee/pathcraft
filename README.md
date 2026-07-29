# PathCraft

**PathCraft is an extensible Go routing engine for maps, transit, simulations, and custom graphs.**

It ships working routing today: OSM street routes via A*, GTFS transit routes via RAPTOR, timetable-aware walk+transit journeys, a 3D air-routing reference, GeoJSON export, a CLI, an HTTP debug/demo server, and one compile-time registry for custom routing modes, loaders, algorithms, exporters, and cost models.

## Features

- **Street routing**: parse OSM XML / `.osm.gz` for one-file graphs, or stream OSM PBF into versioned XYZ chunks for bounded local/regional routing.
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
| `worldgraph` | loader | Versioned local XYZ chunk store   |
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
./bin/pathcraft serve --file testdata/example.osm --gtfs testdata/mini_gtfs --addr 127.0.0.1:8080
# Network exposure is explicit; add authentication/rate limiting at the reverse proxy.
./bin/pathcraft serve --file testdata/example.osm --addr :8080 \
  --cors-origin https://app.example
./bin/pathcraft grpc --file testdata/example.osm --addr 127.0.0.1:9090

# Self-contained 3D plugin; no street graph required.
./bin/pathcraft route --mode air \
  --from-position '12.5648,55.6726,100' \
  --to-position '12.5717,55.6833,200' \
  --opt cruise_altitude_m=1500 --coords
```

### Versioned world graph chunks

Build a local store from an OSM PBF extract, route with existing mode IDs,
then serve viewport-driven graph chunks:

```bash
./bin/pathcraft chunks build --pbf region.osm.pbf --store world --region demo
./bin/pathcraft route --chunks world --mode walk \
  --from-position '12.5683,55.6761' --to-position '12.5685,55.6762'
./bin/pathcraft serve --chunks world --addr 127.0.0.1:8080
```

`make demo-chunks PBF=region.osm.pbf REGION=demo STORE=world` is the
same thin build-and-serve flow. Defaults are routing zoom 12, one-tile initial
halo, 256 route tiles, three expansions, and a 512 MiB decoded cache.
Longitude wraps at the antimeridian; latitude is limited to Web Mercator
`±85.05112878°`.

Each build publishes a new immutable generation. Reimporting the same region
replaces its prior provenance and removes deleted roads; already-open routers
remain pinned to their generation. Uncovered origins, destinations, or
required corridor tiles return an error—there is no straight-line or partial
route fallback. Historical generations are retained; no built-in garbage
collector removes them, so operators must clean only generations no active
reader or manifest needs. This MVP targets bounded local/regional trips, not
arbitrary intercontinental routing or a continental hierarchy.

Generated `.pcg` stores are trusted local artifacts, never HTTP uploads. The
PBF importer applies framing, decompression, structural, coordinate, and
way-size limits before decoding, but still runs as an explicit local build
step. Keep OpenStreetMap attribution visible when rendering derived data:
© OpenStreetMap contributors, under the ODbL. The HTTP server is
unauthenticated and binds loopback by default; explicit non-loopback exposure
needs a trusted reverse proxy, authentication, TLS, and rate limits.

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
    defer res.Close()
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
- `GET /graph` — loaded one-file walking graph as GeoJSON lines
- `GET /graph/chunks/{generation}/{z}/{x}/{y}` — immutable owned-edge GeoJSON for a covered world chunk
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
pkg/plugins               registry, standard plugins, nearest-node index, and worldgraph chunk runtime/builder
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
- CLI flags configure OSM/PBF inputs, world chunk stores/cache/route caps, GTFS directories, endpoints, server address, and optional exact CORS origins.
- `Makefile` variables:
  - `OSM_FILE` (default `testdata/example.osm`)
  - `GTFS_DIR` (default `testdata/mini_gtfs`)
  - `ADDR` (default `127.0.0.1:8080`)
  - `BBOX` / `OUT` for `make fetch-osm`
  - `PBF` / `REGION` / `STORE` / `CHUNK_ZOOM` for `make demo-chunks`
- Parsed graph caches are written as `<osm-file>.cache`; world stores publish `manifest.json` plus immutable `generations/<generation>/<z>/<x>/<y>.pcg`. Both are trusted local artifacts, not upload/network input; versions, checksums, provenance, and atomic manifest replacement reject mixed or stale generations.

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

PathCraft is a prototype routing engine. Library, HTTP, timetable-routing, SDK, WASM, gRPC, plugins, and versioned regional graph chunks work at documented scope, but production hardening remains. Chunk routing is bounded corridor A*, not a planet-scale hierarchy; one-file graphs remain resident, gRPC is local plaintext by default, and WASM street routing is synchronous. Known next steps include GTFS calendars/realtime, richer stop access, regional profiling, API security, and deployment packaging.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Keep changes deterministic, formatted, tested, and aligned with the internal/public package boundaries.

## License

[Apache 2.0](LICENSE)
