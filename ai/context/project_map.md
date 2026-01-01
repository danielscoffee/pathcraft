# Project Map

## Root
- `cmd/pathcraft`: CLI entrypoint.
- `pkg/pathcraft/engine`: **Public API**. The only package external users should import. Orchestrates logic.
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

## Documentation
- `docs/`: Detailed documentation and architecture notes.
- `ai/context/`: High-level context for AI agents (You are here).
