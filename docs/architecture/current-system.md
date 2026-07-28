# Current System Audit

Snapshot of the PathCraft codebase as of the public-API extraction work.
Inventory of `internal/*` packages — these are stable engines that the new
`pkg/pathcraft/*` public layer wraps without rewriting.

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
- `mobility.Profile` interface + `Walking` profile, `DefaultWalkingSpeedMPS`.

## Transport adapters

- `internal/http` — GET-only routing / GTFS endpoints over `engine.Engine`; optional exact HTTP(S)-origin CORS allowlist, disabled by default.
- `internal/grpcapi` — generated `pathcraft.v1` street-route and multimodal-journey service plus standard health checks.
- `internal/wasmapi` — strict JSON bridge for synchronous browser street routes.

## `internal/cli`

- Subcommands: `parse`, `preprocess`, `route`, `transit`, `journey`, `grpc`, `serve`, `plugins`, and `pipeline`.
- `loadEngine(file)` uses cache only when source SHA-256 and cache/preprocessing versions match.

## `pkg/pathcraft/engine`

- High-level `Engine` API: `LoadOSM`, `LoadOSMReader`, `LoadGTFSDir`, `Route`, `RouteByCoordinates`, `TransitRoute`, `MultimodalRoute`, `RouteGeoJSON*`.
- `LoadOSM` publishes parse → graph → contraction preprocessing as one immutable read-mostly graph; loaded engines support concurrent queries, not concurrent reload/mutation.
- `NewWithConfig` validates default street mode, speed, and per-highway penalty multipliers; `New` preserves walking defaults.
- The new `pipeline.go` (`engine.Run`) lives in the same package and drives the registry-backed loader→algorithm→exporter pipeline.

## `pkg/plugins`

- `NearestNodeIndex` interface + `GridNearestNodeIndex` — the only pre-existing
  "plugin" surface, used by `internal/graph`.

## Public extension and ecosystem layers

- `api/pathcraft/v1` — protobuf contract plus generated Go gRPC client/server types.
- `sdk/js/pathcraft.mjs` + `cmd/pathcraft-wasm` — browser loader and Go WebAssembly entrypoint.
- `pkg/pathcraft/core` — interfaces + value types (`Graph`, `Algorithm`, `GraphLoader`, `Exporter`, `CostModel`, `RouteRequest`, `RouteResult`).
- `pkg/pathcraft/registry` — compile-time plugin registry + `Default` global.
- `pkg/pathcraft/plugins/{osmgraph,gtfsgraph}` — adapters from internal types to `core.Graph` with `Native` capability escape hatches.
- `pkg/pathcraft/plugins/{astar,raptor,osm,gtfs,geojson}` — built-in plugins that register with `registry.Default` in `init()`.
