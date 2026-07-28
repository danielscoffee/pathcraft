# Current System Audit

Snapshot of the PathCraft codebase as of the public-API extraction work.
Inventory of `internal/*` packages — these are stable engines that the new
`pkg/pathcraft/*` public layer wraps without rewriting.

## `internal/graph`

- `Graph` (Nodes, Edges, nearestNodeIndex)
- `NodeID int64`, `Node{Lat,Lon}`, `Edge{To,Cost,DistanceM}`
- `NewGraph`, `AddNode`, `AddEdge`, `AddBidirectionalEdge`
- `Neighbors`, `HasNode`, `NearestNode`
- `Save` / `LoadGraph` (gob-encoded cache)
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

## `internal/http`

- HTTP server exposing GET-only routing / GTFS endpoints; consumes `pkg/pathcraft/engine.Engine`.
- Optional exact HTTP(S)-origin CORS allowlist; disabled by default.

## `internal/cli`

- Subcommands: `parse`, `route`, `transit`, `journey`, `serve`, plus the new `plugins` and `pipeline`.
- `loadEngine(file)` handles cache fast-path.

## `pkg/pathcraft/engine`

- High-level `Engine` API (predates the new `pkg/pathcraft/core` layer): `LoadOSM`, `LoadGTFSDir`, `Route`, `RouteByCoordinates`, `TransitRoute`, `MultimodalRoute`, `RouteGeoJSON*`.
- `NewWithConfig` validates default street mode, speed, and per-highway penalty multipliers; `New` preserves walking defaults.
- The new `pipeline.go` (`engine.Run`) lives in the same package and drives the registry-backed loader→algorithm→exporter pipeline.

## `pkg/plugins`

- `NearestNodeIndex` interface + `GridNearestNodeIndex` — the only pre-existing
  "plugin" surface, used by `internal/graph`.

## New public layer (this work)

- `pkg/pathcraft/core` — interfaces + value types (`Graph`, `Algorithm`, `GraphLoader`, `Exporter`, `CostModel`, `RouteRequest`, `RouteResult`).
- `pkg/pathcraft/registry` — compile-time plugin registry + `Default` global.
- `pkg/pathcraft/plugins/{osmgraph,gtfsgraph}` — adapters from internal types to `core.Graph` with `Native` capability escape hatches.
- `pkg/pathcraft/plugins/{astar,raptor,osm,gtfs,geojson}` — built-in plugins that register with `registry.Default` in `init()`.
