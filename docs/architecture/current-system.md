# Current System Audit

Snapshot after public-API extraction and versioned regional graph chunks.
Inventory covers private engines plus public hosts/plugins; `pkg/pathcraft/*`
wraps existing internals without rewriting them.

## `internal/graph`

- `Graph` (Nodes, Edges, nearestNodeIndex, optional ContractionIndex)
- `NodeID int64`, `Node{Lat,Lon}`, metadata-bearing directed edges
- `NewGraph`, `AddNode`, `AddEdge`, `AddBidirectionalEdge`
- `Neighbors`, `HasNode`, `NearestNode`
- deterministic directed degree-two chains with arbitrary-endpoint query arcs
- source-hashed/versioned cache header + gob payload, atomically replaced by `Save`
- Plays well with `pkg/plugins.NearestNodeIndex` via `SetNearestNodeIndex`.

## `internal/osm`

- XML parser (`.osm` / `.osm.gz`): `ParseFile`, `ParseXML`
- `Data{Nodes, Ways}`, `Filter{IncludeHighways}`, `DefaultFilter`
- `BuildGraph(data, filter) *graph.Graph` — the bridge from OSM to graph.

## `internal/gtfs`

- Parsers: `ParseStopTimes(File)`, `ParseTrips(File)`, `ParseStops(File)`, `ParseTransfers(File)`
- Types: `StopID`, `TripID`, `RouteID`, `Stop`, `StopTime`, `Transfer`, `TripToRoute`
- Index: `StopTimeIndex` + `BuildIndex(stopTimes, tripRoutes)` — the RAPTOR-ready structure.

## `internal/routing/astar`

- `AStar(g *graph.Graph, source, target NodeID, h geo.Heuristic) (Path, error)`
- `Path{Nodes, TotalCost, TotalDistance, NodesCount}`
- Heap-based open set. Operates on `*graph.Graph` directly.
- Uses contraction chains when published, then expands every original path node; falls back to base adjacency after graph mutation.

## `internal/routing/raptor`

- `Router{index, transfers}`, `NewRouter(idx, transfers)`
- `Search(source StopID, departure time.Time) *Result`
- `Result{ArrivalTimes, EarliestArrival, Parents}` + `ReconstructPath(target)`
- Reconstructed transit and transfer steps retain scheduled departure and arrival times.
- Round-based RAPTOR with `MaxRounds = 10`.

## `internal/geojson`

- `WriteGraphToGeoJSON`, `PathToGeoJSON`
- Emits `FeatureCollection` with `LineString` geometry.

## `internal/geo`, `internal/time`, `internal/mobility`

- Geometry primitives (`HaversineDistance`, `HaversineHeuristic`)
- Custom `time.Time` (seconds since midnight) + parser
- `mobility.Profile` interface + walking, cycling, and driving profiles.

## `pkg/plugins/worldgraph`

- Streaming regional importer plus restartable global PBF pipeline with
  external reference sorting, compact node lookup, bounded shard writers, and
  shared street-access policy.
- Versioned fixed-zoom XYZ chunks stored as regional `.pcg` files or sparse
  zoom-8 indexes and segmented packs, with stable OSM IDs, edge ownership,
  seam copies, source provenance, checksums, and immutable generations.
- Request-local graph unions, wrapped corridors, bounded expansion,
  profile-aware A*, and a 512 MiB decoded-byte LRU by default.
- Regional replacement removes old provenance/deleted roads; global builds
  publish one source snapshot. Both sync staged files, atomically replace
  `manifest.json`, and leave open routers pinned.
- Coverage is Web Mercator `±85.05112878°`. Missing coverage/files,
  corruption, area limits, no path, and cancellation stay distinct errors.

## Transport adapters

- `internal/http` — GET-only routing / GTFS endpoints over a private mode
  host; legacy `engine.Engine` remains optional. Chunk hosts expose immutable
  owned-edge GeoJSON and `/config` capability metadata.
- `internal/grpcapi` — generated `pathcraft.v1` street-route and multimodal-journey service plus standard health checks.
- `internal/wasmapi` — strict JSON bridge for synchronous browser street routes.

## `internal/cli`

- Subcommands: `parse`, `preprocess`, `chunks build`, `chunks build-global`,
  `route`, `transit`, `journey`, `grpc`, `serve`, `plugins`, and `pipeline`.
- `route` and `serve` accept exactly one legacy `--file` or versioned
  `--chunks` host for street modes. Servers default to `127.0.0.1`.
- `loadEngine(file)` uses cache only when source SHA-256 and cache/preprocessing versions match.

## `pkg/pathcraft/engine`

- High-level API: `LoadOSM`, `LoadOSMReader`, `LoadGTFSDir`, context-aware
  coordinate routing, `Route`, `TransitRoute`, `MultimodalRoute`, and
  `RouteGeoJSON*`.
- `LoadOSM` publishes parse → graph → contraction preprocessing as one immutable read-mostly graph; loaded engines support concurrent queries, not concurrent reload/mutation.
- `NewWithConfig` validates primitive walking speed and per-highway penalty multipliers; high-level mode policy lives in plugins.
- The new `pipeline.go` (`engine.Run`) lives in the same package and drives the registry-backed loader→algorithm→exporter pipeline.

## `pkg/plugins`

- One compile-time registry for modes, algorithms, loaders, exporters, cost models, and loggers.
- Built-in OSM/GTFS/worldgraph loaders plus A*, RAPTOR, GeoJSON, and zap implementations.
- Registered high-level `walk`, `bike`, `car`, `gtfs`, and 2D/3D reference `air` modes.
- `NearestNodeIndex` interface + `GridNearestNodeIndex`, used by `internal/graph`.

## Public extension and ecosystem layers

- `api/pathcraft/v1` — protobuf contract plus generated Go gRPC client/server types.
- `sdk/js/pathcraft.mjs` + `cmd/pathcraft-wasm` — browser loader and Go WebAssembly entrypoint.
- `pkg/pathcraft/core` — dimension-neutral interfaces + values (`Graph`, `Algorithm`, `Mode`, loaders, exporters, requests, N-dimensional positions, and route segments).
- `pkg/plugins/{osmgraph,gtfsgraph}` — adapters from internal types to `core.Graph` with `Native` capability escape hatches.
- `pkg/plugins/*` — registry plus built-in implementations that register with `plugins.Default` in `init()`.
