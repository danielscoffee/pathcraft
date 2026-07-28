# Project Map

## Root

- `api/pathcraft/v1`: Protobuf contract and generated Go gRPC clients.
- `cmd/pathcraft`: Native CLI entrypoint.
- `cmd/pathcraft-wasm`: Browser Go-WASM entrypoint.
- `pkg/pathcraft/engine`: Primary public Go routing API.
- `pkg/pathcraft/{core,registry,plugins}`: Public compile-time plugin interfaces and adapters.
- `sdk/js`: JavaScript loader for browser WASM.
- `web/`: Frontend and visualization tools.

## Internal (`/internal`)

Private core logic.

- `graph/`: Graph data structures (Adjacency lists, Nodes, Edges).
- `geo/`: Geometric calculations (Haversine, etc.).
- `routing/`: Routing algorithms (A*, Dijkstra, etc.).
- `time/`: Time handling with GTFS >24:00:00 support.
- `mobility/`: Mobility profiles and transit domain entities.
- `osm/`: OpenStreetMap data parsing and conversion.
- `gtfs/`: GTFS data parsing (Transit, RAPTOR-ready).
- `geojson/`: GeoJSON export adapters.
- `http/`: HTTP server handlers.
- `grpcapi/`: Protobuf/gRPC adapter and health service.
- `wasmapi/`: Strict JSON bridge for JavaScript/WASM.

## Documentation

- `docs/`: Architecture, SDK/API guides, plans, and status notes.
- `ai/context/`: High-level context for AI agents (You are here).
