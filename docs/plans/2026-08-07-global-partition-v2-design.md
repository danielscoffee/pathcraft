# Global Contribution Partition v2 Design

**Date:** 2026-08-07
**Status:** Approved for implementation

## Goal

Replace the packed-global builder's random node-index lookup loop and its
full directed-edge contribution spool. The published chunk, pack, index, and
manifest formats remain unchanged. Runtime behavior and regional imports remain
unchanged.

The change must let an existing checkpoint that completed `select-nodes`
continue without repeating source validation, way spooling, reference sorting,
or node selection.

## Evidence

The first official-planet attempt produced:

- 228,354,464 routable ways;
- 2,671,633,633 way-node references;
- 2,333,477,374 selected nodes in a 93.3 GB fixed-record index;
- a 38.0 GB way spool.

The old partition replays every reference through `globalNodeIndex.Get`. Each
lookup performs a binary search over the disk index, so the planet path can
issue tens of billions of small `ReadAt` operations. The contribution codec
then repeats two complete nodes, edge metadata, source, distance, owner, and
target tile for each directed edge and halo tile. Two minimum-size records per
candidate segment alone are about 687 GiB before skipped segments or seam
copies.

## Pipeline

Version 2 adds a sequential sort/merge join after the existing selected-node
checkpoint:

1. Replay `ways.spool` and write fixed records `{node_id, occurrence}`. The
   occurrence is the exact zero-based position of a reference across all ways.
2. External-sort requests by `(node_id, occurrence)` with bounded runs and a
   merge fan-in no greater than 64. Duplicates are preserved.
3. Sequentially merge the request stream with `nodes.idx`. Emit fixed resolved
   records `{occurrence, node_id, longitude, latitude}`. No searchable index
   lookup is used in this stage.
4. External-sort resolved records by occurrence. The final stream must contain
   exactly `0..references-1`, with no gaps or duplicates.
5. Replay ways and resolved nodes in lockstep. Validate every node ID and way
   range. Compute current ownership, edge midpoint ownership, restrictions,
   and halo tiles.
6. For each `(way, zoom-8 shard)`, write one keyed compact undirected fragment
   to a sequential journal. Stable multi-pass radix partitioning of the 16-bit
   zoom-8 shard key produces final shard files with bounded descriptors and no
   per-way fsync. A fragment stores policy/text once plus its shard-relevant
   endpoint pairs. It omits derivable distance, owners, directed edges, source,
   and target tiles.
7. Build one shard at a time. Re-expand fragments through the shared segment
   and halo helpers into the existing per-shard contribution store, normalize
   chunks, and write the existing immutable pack/index format.

Inputs may be removed only after their downstream stage is durably
checkpointed. This includes obsolete reference files after node selection, the
node index after the join, and the way/resolved streams after fragment
partitioning. Compact fragment inputs remain in the work directory so
interrupted or damaged staged shards can be rebuilt.

## Work-format compatibility

Published stores need no migration: packed layout version 1, chunk encoding,
generation IDs, manifest fields, URLs, checksums, and publication ordering do
not change.

New stage names use a `-v2` suffix. The hardened work state is version 3. A
version-1 work directory completed only through `select-nodes` is eligible:
source identity, build options, stage frontier, counts, and artifact sizes are
revalidated before atomically adding v2 stages. A checkpoint that
already completed the old `partition-contributions` stage must not be silently
reinterpreted. A published matching generation remains immediately reusable.

## Invariants

- Request count equals the recorded reference count.
- Occurrences form a bijection with `0..N-1`.
- Sorted request and node streams are monotonic; missing references and
  conflicting node records fail.
- Valid OSM coordinates in `[-180,180] × [-90,90]` resolve references. Segments
  touching coordinates outside Web Mercator are omitted without bridging.
- Each routable in-range segment expands to the same two directed edge IDs,
  Haversine distance, mode/oneway flags, midpoint owner, and unique
  endpoint/midpoint halo tiles as the old builder.
- Source is the fixed global provenance `planet`.
- Records, strings, segment counts, sort record payload, merge fan-in, open
  files, and chunk limits remain bounded. Run-path metadata scales with the
  number of external-sort runs, so production builds must not use pathological
  tiny run-memory limits.
- Work files are private regular files, atomically replaced and synced at stage
  boundaries. Final fragment directories are installed atomically after bounded
  sequential radix passes.
- State records the record count, byte count, and SHA-256 of every fragment
  spool; recovery verifies the summary during replay before accepting a pack.
  Cancellation never marks an incomplete stage complete.
- Output is deterministic across run-memory and open-shard limits.

## Resource model

The join replaces random reads with bounded sequential run I/O. At planet
counts, 16-byte requests are about 39.8 GiB and 32-byte resolved records are
about 79.6 GiB. Sort runs temporarily coexist with one input, one run
generation, and one output generation, so stage-wise deletion and high-water
reporting are required.

Fragment partition uses one keyed journal and bounded stable radix passes. One
input and one output bucket generation can coexist; open-shard limits change
pass count rather than this disk overlap. Pack generation streams one chunk at
a time. Its one current-shard contribution database is reconstructible
`NoSync` scratch (avoiding per-batch durability barriers), but still needs a
measured disk budget; the immutable shard files provide recovery durability.

Compact fragment size depends on way length, text, and shard crossings. It must
be reported as amplification per candidate segment and per way-spool byte; no
universal PBF-size multiplier is promised. Final packs still contain required
directed and halo copies and can remain hundreds of GiB.

## Validation

Automated acceptance must cover:

- repeated and signed node IDs, occurrence ties, more than 64 sort runs, tiny
  run memory, truncation, cancellation, and deterministic hashes;
- missing references, decreasing/conflicting nodes, occurrence gaps/duplicates,
  exact coordinate bits, and polar references;
- fragment codec corruption and bounds, radix descriptor limits, deterministic
  stable partitioning, clean-boundary loss, z12 and z8 seams, antimeridian
  ownership, restrictions, and cancellation;
- differential normalized chunks and packed hashes between old expansion and
  v2 fragments for the same fixture;
- resume after every new stage plus rejection of unsafe old downstream state;
- unchanged publication recovery, source re-verification, packed routing, and
  rendering tests.

A pinned representative extract must then compare old and v2 reports on the
same machine and flags. Record per-stage wall time, sequential bytes/records per
second, peak RSS, artifact high-water bytes, fragment amplification, staged
store bytes, and final store bytes. A new official-planet attempt is not
approved until preflight includes filesystem topology, retained generations,
and explicit headroom.
