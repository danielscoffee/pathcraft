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
- `osm/`: OpenStreetMap data parsing and conversion.
- `gtfs/`: GTFS data parsing (Transit).
- `geojson/`: GeoJSON export adapters.
- `http/`: HTTP server handlers.

## Documentation
- `docs/`: Detailed documentation and architecture notes.
- `ai/context/`: High-level context for AI agents (You are here).
