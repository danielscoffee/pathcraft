# Interactive Routing Demo (OSRM-style)

PathCraft ships an interactive Leaflet demo: click two points on the map,
the server snaps them to the nearest OSM nodes, A* finds the optimal
walking route, and the result comes back as GeoJSON rendered on the map.

## Run with the bundled example

```bash
make demo
# open http://localhost:8080/graph-visual
```

`make demo` is shorthand for:

```bash
./bin/pathcraft serve --file examples/example.osm --gtfs examples/mini_gtfs --addr :8080
```

The bundled `examples/example.osm` is tiny (7 nodes) — fine to verify the
demo loads, not impressive visually.

## Run with a real city

Use the `fetch-osm` Makefile target to grab a bounding box from Overpass:

```bash
# defaults: a small slice of central Recife, BR
make fetch-osm
make demo OSM_FILE=examples/recife_central.osm
```

Or override the bbox (south,west,north,east):

```bash
make fetch-osm BBOX="40.7480,-74.0050,40.7620,-73.9700" OUT=examples/manhattan.osm
make demo OSM_FILE=examples/manhattan.osm
```

Tips:
- Keep bboxes small (~1–3 km²) — first load parses XML and writes a
  `<file>.cache` so subsequent runs are instant.
- Overpass rate-limits aggressively. If you get an empty file, wait and retry.

## What you get

- **Click A**, then **click B** → optimal walking route appears.
- Route panel shows distance, estimated walk time, node count, solve time.
- **R** or the Reset button clears markers and route.
- Background grey lines are the full walkable graph from `/graph`.
- Optional GTFS overlay (in the collapsible details section) renders the
  loaded transit stops + per-trip stop-times when a feed is loaded.

## How it works under the hood

```
browser click (lat,lon)
   │
   ▼
GET /route?from_lat&from_lon&to_lat&to_lon
   │
   ▼
engine.RouteByCoordinates
   │   ├─ NearestNode (grid index)
   │   └─ astar.AStar over internal/graph
   │
   ▼
geojson.PathToGeoJSON  →  FeatureCollection LineString  →  Leaflet
```

The new pluggable layer (`pkg/pathcraft/registry` + plugins) backs the
`pathcraft pipeline` CLI. The HTTP demo still uses the classic
`pkg/pathcraft/engine.Engine` directly because it predates the registry
and is already wired into the Leaflet template.
