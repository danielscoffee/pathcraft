# Global Worldgraph Storage Design

**Date:** 2026-07-30
**Status:** Approved; contribution partition stage superseded

The packed runtime/store design remains current. Global build stages 6–7 that
used searchable node-index lookups and full edge-contribution spools are
superseded by [Global Contribution Partition v2](2026-08-07-global-partition-v2-design.md).

## Goal

Build one local, planet-wide, fixed-scale street graph from an immutable
`planet-latest.osm.pbf` snapshot. PathCraft must serve and route from zoom-12
chunks anywhere covered by that snapshot while loading only requested chunks.
The storage implementation remains inside `pkg/plugins/worldgraph`; no external
database or network tile service is required at runtime.

Global means global coverage, not arbitrary intercontinental A*. Routes remain
bounded by the existing tile and expansion limits. Full snapshot replacement is
the only update mechanism in this version; OSM replication diffs are out of
scope.

## Constraints

- Routing and rendering zoom is fixed at 12.
- One local server and local filesystem own the store.
- A full PBF build publishes one immutable generation atomically.
- Runtime startup and request memory must not scale with total zoom-12 tile
  count.
- Runtime must not create or open millions of individual tile files.
- Existing regional per-tile stores remain readable.
- Existing `/graph/chunks/{generation}/{z}/{x}/{y}` URLs remain stable.
- Chunk decoding, checksums, route limits, cancellation, and trusted-local
  `.pcg` boundaries remain unchanged.

## Packed Layout

Zoom-12 tiles are grouped by their zoom-8 parent. One parent contains 16×16,
or 256, zoom-12 children. Dense shards split into deterministic pack segments
before a segment exceeds 1 GiB.

```text
world/
  manifest.json
  .publish.lock
  generations/<generation>/
    metadata.json
    shards/<x-high>/<x>/<y>.idx
    shards/<x-high>/<x>/<y>-000.pack
    shards/<x-high>/<x>/<y>-001.pack
```

`manifest.json` remains the atomic generation pointer. Packed manifests add:

- `layout: "packed"`;
- `layout_version: 1`;
- `shard_zoom: 8`;
- sorted occupied zoom-8 shard IDs;
- source name, SHA-256, byte size, and build timestamp;
- global covered bounds derived from occupied shards.

They do not contain every zoom-12 tile or per-tile region membership. Legacy
manifests omit `layout` and retain existing behavior.

Each shard index has a fixed header and at most 256 fixed-size entries. An
entry records presence, pack segment number, byte offset, byte length, and
SHA-256. Pack records are complete existing encoded `.pcg` chunks, so chunk
format and structural validation stay shared.

## Runtime

`Store` detects layout from the manifest. Legacy stores keep direct tile-file
reads. Packed stores:

1. derive the zoom-8 parent and local 0–255 slot from a zoom-12 tile;
2. load the small shard index through a bounded index LRU;
3. reject an absent slot as uncovered;
4. open the referenced immutable pack segment;
5. use `ReadAt` with the indexed offset and length;
6. verify the index digest, then run the existing chunk decoder and provenance
   checks.

Router coverage checks call `Store.Covers` instead of constructing a map of
all covered zoom-12 tiles. Existing decoded chunk cache, request-local graph
union, nearest-node behavior, route corridor limits, and HTTP rendering remain
unchanged.

`/config` derives chunk-only map center and bootstrap zoom from packed coverage,
so synthetic and real stores open over covered data instead of the Recife
fallback.

## Global Build Pipeline

A planet build uses bounded-memory, restartable stages:

1. Open one regular PBF descriptor and record identity and size.
2. Validate and SHA-256 the same descriptor; each later pass uses a section
   reader over that descriptor.
3. Stream routable ways, writing a length-delimited way spool and fixed-width
   node-reference records.
4. External-sort and deduplicate node references in bounded-memory runs.
5. Stream monotonic node IDs from the official planet PBF and merge them with
   sorted references, writing a compact fixed-record referenced-node index.
   Reject decreasing IDs or missing references.
6. Replay the way spool. Resolve coordinates by binary search in the compact
   node index, apply current ownership/halo rules, and append contributions to
   zoom-8 shard spool files through a bounded file-handle LRU.
7. Create a resumable store-owned `generations/.<generation>.build` directory.
   Process one shard spool at a time into at most 256 normalized chunks, writing
   pack segments and indexes directly into that directory.
8. Verify source identity and SHA-256 again before publication.
9. Acquire the existing cross-process writer lock, compare the expected parent,
   validate and sync the completed stage, rename it to the immutable generation,
   then atomically replace `manifest.json`.

No generation-wide copy occurs. Temporary sort/spool data lives in the selected
work directory; final pack bytes are written once on the store filesystem.

The official planet PBF is expected to have monotonic node IDs. Regional or
custom unsorted PBFs continue using the existing regional builder.

Stage metadata in the requested work directory records source identity,
configuration, completed stages, and the store-owned generation-stage path.
Re-running the same command resumes only when metadata matches; mismatches fail
rather than mixing sources. External merges use bounded fan-in, so run count
cannot exhaust file descriptors.

## CLI

```text
pathcraft chunks build-global \
  --pbf planet-latest.osm.pbf \
  --store world/ \
  --work-dir /fast-ssd/pathcraft-world-build
```

Optional controls:

```text
--run-memory-mb 512
--pack-mb 1024
--open-shards 64
--resume=true
```

Zoom is not configurable for global stores in layout version 1.

## Failure and Resource Policy

- Cancellation stops the current stage and preserves resumable work.
- No live manifest changes before the final commit point.
- Short reads, offset overflow, duplicate slots, digest mismatch, corrupt
  indexes, or corrupt chunks are typed corruption errors.
- External-sort memory, open shard files, pack size, shard chunk counts, and
  per-way contribution expansion are bounded.
- Insufficient disk fails before publication; existing generation remains
  usable.
- A committed generation is immutable. Failed post-commit durability rollback
  retains generation files needed by readers, matching existing semantics.

## Validation

Automated acceptance includes:

- zoom-12 to zoom-8 shard/slot math, including antimeridian edges;
- deterministic binary index round trips and malformed-index rejection;
- pack splitting, random access, digest checks, and short-read rejection;
- packed manifest validation without a global tile list;
- legacy store compatibility;
- bounded index-cache concurrency and cancellation;
- packed-store routing/rendering across tile and shard seams;
- external sort/dedup under tiny memory limits;
- monotonic-node and missing-reference rejection;
- resumable stage metadata mismatch rejection;
- atomic packed publication and stale-writer conflict;
- multi-region synthetic global PBF build and loopback HTTP viewport requests;
- race tests, full Go checks, frontend tests, vulnerability scans, native build,
  and WASM build.

A manual planet acceptance run records source hash, wall time, peak RSS, work
bytes, final store bytes, occupied shards, chunks, nodes, and edges. No CI test
requires downloading planet data.
