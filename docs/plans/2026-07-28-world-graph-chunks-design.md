# Worldwide Graph Chunks Design

**Date:** 2026-07-28
**Status:** Approved

## Goal

Support local and regional routing anywhere covered by offline-imported OSM regions without loading one monolithic graph. Existing `walk`, `bike`, and `car` mode plugins must route transparently across chunk boundaries. The web Streets layer must render only visible chunks.

Worldwide means coverage may be assembled incrementally from regional PBF extracts. Continental hierarchy, arbitrary planet-scale routes, runtime network downloads, partial routes, and direct-line fallback are out of scope.

## Architecture

Add `pkg/plugins/worldgraph` without changing `core.Mode`.

`worldgraph.Loader` registers through the existing `core.GraphLoader` extension point. Its source is a versioned XYZ chunk store:

```text
world/
  manifest.json
  12/<x>/<y>.pcg
```

A `worldgraph.Router` opens the store and satisfies the same coordinate-routing host capability consumed by standard street modes. `pathcraft serve --chunks world/` selects that host. Existing `--file` routing remains supported.

Routing uses request-local graph unions rather than one mutable global engine graph. This keeps decoded chunks immutable, permits safe LRU eviction, and avoids cross-request mutation.

## Chunk Scheme

Use fixed-zoom Web Mercator XYZ chunks, default zoom 12 and latitude range `±85.0511°`.

Each directed edge has one owner tile. Nodes and edges required to cross seams are copied into neighboring halos. Stable OSM node and way IDs allow deterministic deduplication. Render responses include owned edges only, preventing duplicate lines from halos.

Longitude wraps across the antimeridian. Latitude clamps to Web Mercator bounds.

## Store Manifest and Chunk Format

Store manifest records:

- format and preprocessing versions;
- routing zoom and CRS bounds;
- store generation;
- imported region names and source SHA-256 values;
- tiles touched by each region;
- build timestamp.

Each `.pcg` records:

- stable OSM node IDs and coordinates;
- directed edges with way ID, endpoints, distance, highway metadata, and mobility restrictions;
- owner tile;
- source-region provenance;
- checksum.

Encoding follows PathCraft's existing trusted local graph-cache pattern: magic header, explicit version, structural validation, temporary file, sync, and atomic rename. Chunk files are build artifacts and are never accepted as HTTP uploads.

## PBF Build Pipeline

Add:

```text
pathcraft chunks build --pbf region.osm.pbf --store world/ --region <name>
```

Builder uses a maintained streaming Go PBF decoder and temporary disk-backed node index. No external `osmium` process is required.

Pipeline:

1. Stream nodes into temporary ID-to-coordinate index.
2. Stream ways, retain routable highways, resolve references, and create directed edges.
3. Assign edge ownership and seam halos.
4. Stage affected chunks outside live store.
5. Merge overlap by stable edge identity.
6. Remove prior provenance when replacing the same region.
7. Atomically publish chunks, then manifest.

Manifest tracks prior region tile membership so deleted roads disappear during replacement. Shared border edges survive while another region still references them.

Malformed coordinates, excessive way-node counts, unsupported versions, corrupt checksums, cancellation, or insufficient disk fail before manifest publication. Existing generation remains usable.

## Runtime Routing

Per request:

1. Validate coordinates.
2. Compute shortest wrapped origin-destination XYZ corridor.
3. Load corridor plus one-tile halo.
4. Decode and checksum through shared LRU cache.
5. Build a request-local deduplicated graph union.
6. Snap endpoints and run existing profile-aware A*.
7. Expand one tile ring when no path exists.
8. Stop on route, cancellation, missing coverage, corruption, or configured cap.

Defaults:

```text
routing zoom:        12
initial halo:        1 tile
expansion step:      1 tile
maximum route area:  256 tiles
decoded cache:       512 MiB
```

Expose:

```text
--chunk-cache-mb
--route-max-tiles
--route-max-expansions
```

Cache capacity is measured by decoded bytes. Active requests retain references after eviction. Concurrent reads for one uncached tile share one load; failures are not cached permanently.

Manifest distinguishes covered empty tiles, uncovered tiles, expected files missing from disk, and corrupt/version-incompatible files. Router never treats missing coverage as empty terrain.

Existing coordinate routing gains context-aware entry point. Current non-context API remains a wrapper. Street modes propagate request cancellation.

## HTTP and Web Rendering

`/config` gains optional capability:

```json
{
  "graph_chunks": {
    "generation": "sha256-prefix",
    "zoom": 12,
    "min_render_zoom": 11,
    "url": "/graph/chunks/{generation}/{z}/{x}/{y}"
  }
}
```

Endpoint:

```text
GET /graph/chunks/{generation}/{z}/{x}/{y}
```

Response is GeoJSON containing owned edges. Generation in path permits immutable browser and CDN caching.

Status mapping:

- invalid tile or coordinates: `400`;
- uncovered tile: `404`;
- stale generation: `409`;
- corrupt or missing expected file: `503`.

Existing Streets toggle becomes viewport-driven:

- compute visible routing tiles on pan and zoom;
- fetch at most six concurrently;
- abort stale generations;
- add chunks progressively;
- remove offscreen layers;
- retain bounded 64-tile browser LRU;
- render nothing below configured zoom threshold;
- derive legend from visible edges.

## Error Policy

No fallback geometry or partial street route is returned.

Typed failures distinguish:

- missing coverage;
- route-area limit reached;
- no connected path;
- corrupt chunk;
- cancelled request.

## Validation

Required checks:

- XYZ math, poles, and antimeridian;
- PBF decoding and malformed-input rejection;
- seam ownership and cross-tile routing;
- overlapping import merge, replacement, and deletion;
- failed build preserving prior generation;
- cache eviction and concurrent-load deduplication;
- corridor expansion, cancellation, and limits;
- HTTP generation and status behavior;
- frontend viewport selection, stale abort, and progressive rendering;
- end-to-end tiny PBF route crossing a tile boundary;
- Go race tests, frontend checks, vulnerability scans, native build, and WASM build.

Acceptance demo:

```bash
pathcraft chunks build --pbf region.osm.pbf --store world --region demo
pathcraft serve --chunks world
```

Enable Streets, pan through covered tiles, and route across a chunk seam using existing mode IDs.
