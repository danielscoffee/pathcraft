# Interactive Routing Demo (OSRM-style)

PathCraft ships an interactive Leaflet demo: click two points, then every registered routing-mode plugin resolves independently. The browser consumes plugin manifests and generic route segments; Leaflet converts those segments to map geometry at the visual boundary.

## Run with the bundled example

```bash
make demo
# open http://localhost:8080/graph-visual
```

`make demo` is shorthand for:

```bash
./bin/pathcraft serve --file testdata/example.osm --gtfs testdata/mini_gtfs --addr 127.0.0.1:8080
```

The tracked `testdata/example.osm` is tiny (7 nodes) — fine to verify the
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

- **Click A**, then **click B** → registered modes resolve progressively.
- Tabs, colors, durations, distance, and options come from plugin manifests/results.
- Built-in air routing returns altitude-preserving 3D positions; Leaflet displays their 2D projection.
- **R** or the Reset button clears markers and route.
- Background grey lines are the full walkable graph from `/graph`.
- Optional GTFS overlay (in the collapsible details section) renders the
  loaded transit stops + per-trip stop-times when a feed is loaded.

## How it works under the hood

```
browser click (lat,lon)
   │
   ├─ GET /modes
   │
   ▼
GET /mode-route?mode=<plugin>&from=lon,lat&to=lon,lat
   │
   ▼
pkg/plugins registry → core.Mode.Route
   │
   ▼
N-dimensional route segments → web visual adapter → Leaflet
```

Legacy `/route` and `/journey` endpoints remain compatibility adapters, but they dispatch registered modes rather than owning mode policy.
