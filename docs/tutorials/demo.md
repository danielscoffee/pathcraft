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

## Run with versioned regional chunks

Build from a local OSM PBF extract and serve the same UI/mode IDs:

```bash
./bin/pathcraft chunks build --pbf region.osm.pbf --store world --region demo
./bin/pathcraft serve --chunks world --addr 127.0.0.1:8080
# equivalent thin wrapper:
make demo-chunks PBF=region.osm.pbf REGION=demo STORE=world
```

The Streets toggle reads visible routing tiles progressively, with at most six
requests in flight and a 64-tile browser LRU. It renders nothing below the
server's minimum graph zoom. Single-file servers omit `graph_chunks` from
`/config`, so the UI retains its one-shot `/graph` fallback.

Chunk defaults are zoom 12, one-tile initial halo, 256 route tiles, three
corridor expansions, and 512 MiB decoded server cache. Web Mercator limits
coverage to `±85.05112878°`; antimeridian viewports wrap. Rebuilding a region
publishes an immutable generation and removes absent roads. Missing coverage
is an error, not an empty/direct-line fallback. Bounded corridors target
local/regional routes, not intercontinental trips.

Generated stores are trusted local artifacts, not upload endpoints. Keep
`© OpenStreetMap contributors` attribution visible. Server default is loopback
and unauthenticated; non-loopback use requires explicit `--addr` plus trusted
TLS/auth/rate limiting infrastructure.

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
- Tabs, colors, metrics, and options come from plugin manifests/results.
- Air routing retains 3D positions; Leaflet displays their 2D projection.
- **R** or the Reset button clears markers and route.
- Street lines use visible immutable `/graph/chunks/...` tiles when available,
  or full `/graph` for legacy single-file mode.
- Optional GTFS overlay (in the collapsible details section) renders the
  loaded transit stops + per-trip stop-times when a feed is loaded.

## How it works under the hood

```text
browser click (lat,lon)
   │
   ├─ GET /config + /modes
   │
   ▼
GET /mode-route?mode=<plugin>&from=lon,lat&to=lon,lat
   │
   ▼
pkg/plugins registry → core.Mode.Route
   │
   ▼
N-dimensional route segments → web visual adapter → Leaflet

Streets toggle → visible XYZ IDs → immutable owned-edge GeoJSON chunks
```

Legacy `/route` and `/journey` endpoints remain compatibility adapters, but they dispatch registered modes rather than owning mode policy.
