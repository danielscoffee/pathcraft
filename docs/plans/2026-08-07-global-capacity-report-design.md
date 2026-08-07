# Global Worldgraph Capacity Report and Preflight Design

**Date:** 2026-08-07

**Status:** Approved follow-up; not implemented
**Gate:** Required before another official-planet continuation, not required for
synthetic partition-v2 semantic acceptance

## Goal

Make global-build disk decisions reproducible and machine-checkable without
pretending that PBF size predicts road density, fragment amplification, pack
size, or RSS. The report must distinguish observed facts, exact values derived
from counts, conservative bounds, operator budgets, and unknowns.

This work is separate from the partition-v2 semantic pipeline. Until it and a
pinned representative run pass, `build-global` remains explicitly not approved
for the official planet checkpoint.

## Non-goals

- no universal PBF-size or representative-extract multiplier;
- no claim that `--run-memory-mb` is an RSS limit;
- no automatic generation garbage collection;
- no general metrics/benchmark service;
- no silent approval when any required output budget is unknown.

## Inputs and outputs

Add an optional version-1 capacity plan with explicit operator budgets:

- maximum keyed fragment-radix journal bytes;
- maximum new packed-generation bytes;
- maximum one-shard contribution-database scratch bytes;
- minimum free bytes and percentage headroom;
- optional SHA-256 of the representative report that justified the budgets.

The builder emits an atomically replaced, mode-`0600`, version-1 JSON report.
Capacity policy does not enter deterministic build-state identity: operators may
increase safety margins on resume. A report is reusable only when source,
generation, pipeline version, and build resource options match.

Every byte-valued estimate carries one basis:

- `observed`;
- `exact_derived`;
- `conservative_bound`;
- `operator_budget`;
- `unknown` (represented as JSON `null`, never zero).

The report includes:

- build/source/options and embedded VCS identity;
- one record per process attempt, including status and process peak RSS when
  supported;
- stage duration, records, logical reads/writes, and observed filesystem minima;
- aggregate artifact lifetimes rather than one row per shard;
- PBF/work/store filesystem topology, available bytes/inodes, and roles;
- final fragment bytes/counts, radix journal/scratch, maximum pack scratch,
  staged generation, final store, and retained generations;
- a tri-state per-filesystem preflight with named components and deficits.

`GlobalCounts.WorkBytes` and `StoreBytes` retain their current checkpoint-snapshot
meaning. They are not relabeled as peaks.

## Filesystem model

Resolve PBF, work, and store paths to filesystem device IDs, using the nearest
existing parent for a target that does not exist. Use available blocks
(`statfs.Bavail`), not total free blocks. If roles share a device, sum their
additional demands once against that device's available bytes. Existing PBF and
retained files already consume the reported available space and are listed for
provenance, not double-counted as additional demand.

A preflight is:

- `fail` if any known required device demand plus headroom exceeds availability;
- `unknown` if no known deficit exists but any required budget or topology value
  is unknown;
- `pass` only if all required values are known and every device fits.

Required mode returns distinct insufficient-capacity and unknown-capacity
errors. Advisory mode reports the result without changing existing library/test
behavior. Preflight is not a reservation; operators must still monitor quotas,
copy-on-write behavior, concurrent writers, bytes, and inodes.

## Artifact lifetime model

Model the actual durable deletion frontier rather than summing every artifact
ever created.

At the official count of 2,671,633,633 references:

- request records: `N * 16 = 42,746,138,128` bytes;
- resolved records: `N * 32 = 85,492,276,256` bytes;
- a fixed sort can overlap input, one run generation, and output (`3N` bytes);
- request-sort work bound after node selection:
  `ways + nodes.idx + 3*requests = 259,620,631,359` bytes;
- resolved-sort work bound after the join:
  `ways + 3*resolved = 294,519,950,783` bytes.

The radix journal is final fragment frames plus a two-byte shard key per
fragment. One input bucket generation and one output generation coexist, so a
plan reserves up to twice its journal budget. `MaxOpenShards` affects pass count
and I/O, not this overlap bound.

Packing overlaps final fragments, one current shard's temporary contribution
database, the growing staged generation, and retained old generations. The
database is reconstructible `NoSync` scratch; immutable index/pack files, not
scratch transactions, are the recovery boundary. The
staged target becomes the final target by rename and is not counted twice. On
resume, already allocated target bytes reduce the additional generation demand.

## Instrumentation hooks

Use event accounting, not recursive tree walks per shard:

1. inventory topology and retained data once at attempt start;
2. register/replace/remove artifacts at durable stage boundaries;
3. observe fixed-sort runs before consumed generations are removed;
4. observe radix journal/output counters before parent-bucket deletion;
5. observe the temporary contribution database before deletion and add only the
   just-synced shard's index/pack bytes;
6. sample O(1) filesystem availability before every major stage and packed
   shard;
7. sample process high-water RSS from a platform-correct source, leaving it
   unknown where unsupported and documenting that page cache/cgroup memory is
   outside its scope;
8. throttle in-progress report fsyncs, but always persist at boundaries and
   before a capacity failure.

The official run also records external OS/cgroup or GNU `time -v` telemetry
because a process-local sample can be lost on host failure and excludes page
cache.

## Proposed CLI

- `--capacity-report FILE` (`-` emits JSON and moves human progress to stderr);
- `--capacity-plan FILE`;
- `--require-disk-preflight`;
- `--preflight-only` (strictly read-only: it must not create or migrate state,
  stores, manifests, or work artifacts).

Human output is limited to `PASS`, `FAIL`, or `UNKNOWN`, then one line per device
with roles, availability, additional requirement, headroom, deficit, and named
unknown budgets.

## Acceptance

Automated tests must cover every resume frontier and artifact lifetime; shared
and separate devices; nonexistent targets; available-vs-free blocks; exact
boundary, deficit, unknown, and overflow behavior; sort/radix/pack observation;
atomic strict JSON; canceled/failed attempt reports; and read-only preflight.

A pinned representative extract then supplies the three unknowable operator
budgets on the same machine and flags intended for the planet build. The next
official attempt is approved only when the reviewed plan, filesystem topology,
retained generations, external RSS evidence, and explicit headroom all pass.
