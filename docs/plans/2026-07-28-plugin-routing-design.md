# Plugin-First Routing Design

## Product rule

Everything optional is a plugin. `core` stays transport-, storage-, UI-, and dimension-neutral. A third party must be able to add road, rail, sea, air, indoor, or orbital routing by implementing public contracts and registering a package; PathCraft adapters discover capabilities instead of switching on built-in mode names.

## Package layout

- `pkg/pathcraft/core`: minimal contracts and shared values only.
- `pkg/plugins`: one public registry for algorithms, loaders, exporters, cost models, loggers, nearest-node indexes, and routing modes.
- `pkg/plugins/<name>`: built-in plugin implementations. Existing `pkg/pathcraft/plugins/*` and `pkg/pathcraft/registry` move here.
- `internal/*`: private implementations wrapped by built-in plugins.
- HTTP, CLI, gRPC, WASM, and web: consumers/adapters. They may translate transport formats but must not own routing-mode policy.

This is a pre-release breaking package cleanup. No compatibility forwarding packages are retained.

## Generic routing-mode contract

`core.Mode` is a high-level registered capability:

```go
type Mode interface {
    Name() string
    Manifest() ModeManifest
    Route(context.Context, any, ModeRequest) (ModeResult, error)
}
```

`host any` is the only runtime escape hatch. Core cannot depend on `engine.Engine` or predict future plugin resources. Built-in plugins assert the smallest host capability they need; self-contained plugins such as air routing ignore it. Custom applications may pass their own host.

Requests contain two N-dimensional positions and string options. Core assigns no coordinate semantics. Each manifest declares CRS, axis names, supported dimensions, display metadata, and option descriptors. Results contain one or more generic route segments. Segment positions remain N-dimensional and carry plugin-supplied presentation metadata. No GeoJSON, Leaflet, OSM, GTFS, latitude, longitude, or HTTP type enters core.

## Standard modes

- `walk`, `bike`, `car`: wrappers around public engine street routing with plugin-owned profiles and manifests.
- `gtfs`: wrapper around multimodal engine routing; emits generic walk/transit/transfer segments.
- `air`: independent 3D reference plugin. It accepts geographic 2D/3D endpoints, adds configurable cruise altitude, returns climb/cruise/descent positions, and proves the contract is not road-only.

All register with `pkg/plugins.Default` from `init()`. Custom executables activate a plugin through a blank import, matching existing compile-time plugin behavior.

## Adapter behavior

HTTP adds a generic mode endpoint. `/modes` is generated from registry manifests. `/mode-route` accepts mode, N-dimensional from/to positions, and options, dispatches the selected plugin, and returns `ModeResult`. Existing `/route` and `/journey` remain compatibility adapters during this change.

Web loads manifests, invokes the generic endpoint for every registered mode, converts generic segments to GeoJSON only at the Leaflet boundary, and uses manifest/segment presentation metadata. Unknown icon keys use a generic fallback. Departure-time UI is driven by an option descriptor, not `gtfs` checks.

## Acceptance evidence

- No imports of `pkg/pathcraft/registry` or `pkg/pathcraft/plugins` remain.
- Registry tests cover mode registration, duplicate rejection, lookup, and sorted discovery.
- Standard mode tests cover walk/bike/car/GTFS adaptation.
- Air test proves three-dimensional positions survive registration, HTTP dispatch, and result encoding.
- HTTP tests prove dynamic manifests and generic dispatch.
- Frontend tests prove manifest-driven requests and segment conversion without mode-name speed tables or GTFS request branching.
- `go test ./...`, frontend tests, lint, production build, diagnostics, and a real HTTP air request pass.

## Explicit limits

This change provides a 3D-capable contract and air route plugin, not a full 3D renderer, flight planner, terrain model, restricted-airspace database, or production deployment. Leaflet projects air longitude/latitude and ignores altitude visually; altitude remains present in API results for a future 3D consumer.
