# World Graph Operations

PathCraft supports two immutable local-store layouts:

- `pathcraft chunks build` imports one regional PBF into legacy per-tile `.pcg`
  files and supports later region replacement.
- `pathcraft chunks build-global` imports one complete PBF snapshot into sparse
  zoom-8 shards containing indexed, segmented zoom-12 chunk packs.

Both layouts route bounded local trips. Global storage removes the need for one
resident planet graph or one file per possible zoom-12 tile; it does not add a
long-distance routing hierarchy.

## Build a packed global store

Build the binary first, then give each build a persistent private work directory:

```bash
make build
./bin/pathcraft chunks build-global \
  --pbf /data/planet-latest.osm.pbf \
  --store /srv/pathcraft/world \
  --work-dir /srv/pathcraft/work/planet \
  --run-memory-mb 512 \
  --pack-mb 1024 \
  --open-shards 64
```

This is the build interface, not approval to continue the current official
planet checkpoint. Complete the capacity gates below first.

Global routing zoom is fixed at 12; `--zoom` exists only to reject accidental
other values. Resource flags mean:

| Flag | Default | Effect |
|---|---:|---|
| `--run-memory-mb` | 512 | Approximate fixed-record payload memory per external-sort run; not a process RSS limit |
| `--pack-mb` | 1024 | Maximum size of each immutable pack segment, not total store size |
| `--open-shards` | 64 | Maximum radix-partition output descriptors; one sequential input is additional and fan-out caps at 64 |
| `--resume` | true | Reuse matching completed stages and packed shards |

Progress names each stage and ends with generation, shard, chunk, way,
reference, node, candidate-segment, compact-fragment, final-fragment-byte,
edge, logical-contribution, current work-byte, current store-byte, and elapsed
totals.

## Resume contract

`WORK_DIR/build-state.json` records source identity, resource options,
generation, completed stages, packed shards, and counts. Rerun the exact command
after cancellation, process failure, or host restart. Resume requires all of:

- same canonical PBF path, size, and SHA-256;
- same fixed routing/shard zooms;
- same `--run-memory-mb`, `--pack-mb`, and `--open-shards` values;
- intact work artifacts and staged generation.

A mismatch fails without reusing data. `--resume=false` also rejects an existing
state; use a fresh empty work directory for a clean build. The source is held by
one stable descriptor and reverified before publication, so replacing or
modifying the PBF during a build fails.

Completed stages are:

1. source validation;
2. way/reference spooling;
3. external unique-reference sort;
4. referenced-node selection;
5. way-reference occurrence spooling;
6. occurrence sort by node ID;
7. sequential occurrence/node merge;
8. resolved-node sort back to way order;
9. compact per-shard fragment partitioning;
10. per-shard pack writing;
11. atomic publication.

Stages 5–9 are partition pipeline v2. They replace per-reference binary searches
against `nodes.idx` with sequential external sort/merge I/O. A work-state v1
checkpoint is atomically upgraded to state v3 only after its completed-stage
frontier, fixed-size artifacts, and way/reference counts pass validation. The
official checkpoint completed through stage 4 and is eligible. Intermediate
state v2 was never released and is rejected rather than guessed.
An unpublished state that completed the old `partition-contributions` stage is
rejected rather than reinterpreted; use a fresh work directory for that case.
Published packed stores are unchanged and need no migration.

Synced shard files are their own recovery journal. A resumed pack stage validates
all occupied shards once and skips valid ones instead of rewriting build state or
recursively measuring the work tree after every shard. Consumed references,
node indexes, way spools, occurrence files, and sort runs are removed only after
their downstream checkpoint is durable.

State v3 records each final fragment spool's shard, record count, byte count, and
SHA-256. A rebuilt shard verifies that summary during the same sequential replay;
clean record-boundary loss cannot silently publish a shorter graph. Fragment
spools remain in the work directory so staged shards can be rebuilt, including
after publication, until the operator intentionally removes the work directory.

## Store and work layout

Published packed stores look like:

```text
STORE/
  manifest.json
  generations/<generation>/
    shards/<x-prefix>/<shard-x>/<shard-y>.idx
    shards/<x-prefix>/<shard-x>/<shard-y>-000.pack
    shards/<x-prefix>/<shard-x>/<shard-y>-001.pack
    ...
```

Manifest fields `layout: "packed"`, `layout_version`, `shard_zoom: 8`, and
`shards` select this format. Packed manifests deliberately omit global zoom-12
`tiles` arrays, including region tile arrays. Each shard index maps a local
zoom-12 tile ID to one segment offset, length, and checksum. Pack and index
files are immutable after publication.

`manifest.json` is the only publication pointer. Publication syncs staged
files, renames the complete generation, then atomically replaces the manifest.
Already-open routers remain pinned to their generation. Concurrent stale
publishers fail rather than overwrite a newer manifest.

Legacy regional generations remain readable. Their files stay under
`generations/<generation>/<z>/<x>/<y>.pcg`.

A partition-v2 work directory adds transient files similar to:

```text
WORK/
  ways.spool                  # removed after fragment partition
  references.raw              # removed after referenced-node selection
  references.sorted           # removed after referenced-node selection
  nodes.idx                   # removed after the sequential join
  way-node-requests.raw       # removed after request sort
  way-node-requests.sorted    # removed after the sequential join
  resolved-way-nodes.raw      # removed after occurrence sort
  resolved-way-nodes.sorted   # removed after fragment partition
  fragments-v2/
    .fragment-radix-work/     # exists only while partitioning
    fragments/<x-prefix>/<x>/<y>.spool
  build-state.json            # includes final-spool summaries
```

## Capacity planning

Work space can exceed final store size because it holds raw and sorted unique
references, the way spool, the compact selected-node index, occurrence sort
runs, resolved-node sort runs, compact fragment spools, and a staged generation
at different points in the pipeline. Publication can also coexist with all
previously retained generations. PBF compression ratios and road density vary
too much for one safe source-size multiplier.

At the recorded planet count of 2,671,633,633 references, the 16-byte
occurrence stream is 42,746,138,128 bytes (39.8 GiB) and the 32-byte resolved
stream is 85,492,276,256 bytes (79.6 GiB). A fixed sort can temporarily retain
its input, one complete run generation, and its final output: 119.4 GiB for the
request sort and 238.9 GiB for the resolved sort, before other live artifacts.
With the current deletion frontier, the corresponding work-tree bounds from the
stage-4 planet counts are 259,620,631,359 and 294,519,950,783 bytes. The PBF,
store, retained generations, filesystem metadata, and required headroom are
additional when they share a filesystem.

Fragment size is data-dependent. Partitioning first writes one keyed sequential
journal, then stable-partitions its 16-bit shard key in bounded radix passes; it
does not perform random per-shard append/fsync churn. One input generation and
one output generation coexist, so radix scratch can approach twice the keyed
journal. The final format stores way policy/text once per `(way, shard)` plus
undirected endpoint pairs, then reconstructs directed halo copies while packing.
Packing streams one normalized chunk at a time instead of retaining all 256
possible chunks in a shard. Its current-shard contribution database is
reconstructible scratch and does not fsync every transaction; only the completed
index/pack shard becomes a synced recovery artifact. A host failure therefore
rebuilds at most the current shard.

The seam regression is byte-identical to the legacy pack output and uses 232
final fragment bytes instead of 1,372 legacy contribution-spool bytes. That tiny
result proves semantics and format amplification, not planet capacity. Printed
`Work bytes` and `Store bytes` are current checkpoint snapshots, not observed
high-water marks; sort runs and radix generations may be created and removed
between them.

Before a planet build:

1. run one pinned representative extract with production resource flags and
   external process/OS telemetry;
2. record stage wall time, logical I/O, peak RSS, filesystem available-byte
   minima, radix journal/scratch bytes, maximum per-shard pack scratch, staged
   generation bytes, and final store bytes;
3. create a versioned capacity plan with explicit fragment, pack-scratch, new
   generation, retained-generation, and free-space headroom budgets;
4. aggregate those budgets by filesystem device: PBF, work, and store paths may
   share the same available bytes;
5. fail a disk preflight on a known deficit and treat any unknown required
   budget as not approved;
6. monitor free bytes, inode counts, and external peak RSS during the build.

PathCraft does not yet emit that machine-readable high-water report or enforce
that preflight. The approved follow-up is
[Global Worldgraph Capacity Report and Preflight](plans/2026-08-07-global-capacity-report-design.md). Therefore the official planet checkpoint must not be continued
merely because the fixed-sort bounds fit the current drive. Capacity reporting,
a representative run, and explicit topology/headroom approval remain hard gates.

`--run-memory-mb` controls the approximate payload of each external-sort run
only. Decoder buffers, indexes,
shard writers, Go runtime overhead, and OS page cache contribute additional RSS.
Lower it when measured RSS is too high. Lower `--open-shards` when descriptor
limits are tight. Lower `--pack-mb` only when deployment tooling needs smaller
objects; it creates more files and index segment references.

No built-in garbage collector removes old generations or work directories.
Remove them only after confirming no active process is pinned to them and no
resume is needed.

## Serving and failure recovery

Serve either layout through the same runtime:

```bash
./bin/pathcraft serve --chunks /srv/pathcraft/world --addr 127.0.0.1:8080
```

`GET /config` reports generation, chunk zoom, minimum render zoom, and a
coverage-derived bootstrap viewport. `GET
/graph/chunks/{generation}/{z}/{x}/{y}` returns immutable GeoJSON with a
one-year cache policy. Uncovered chunks return 404, stale generation URLs return
409, and missing, corrupt, or checksum-mismatched expected chunks return 503.

On cancellation or out-of-space failure, preserve both store and work
directories, correct the cause, then rerun the exact build command. Do not edit
`build-state.json`, indexes, packs, or staged metadata.

On checksum failure:

1. stop routing traffic to the affected store;
2. retain logs and identify the damaged filesystem or transfer path;
3. rebuild from the verified source PBF into a fresh store and work directory;
4. run acceptance checks below;
5. switch deployment to the fresh store;
6. retire the damaged store only after pinned readers stop.

Rebuilding the same source in place is not repair: its deterministic generation
may already be published, and published artifacts are immutable.

## Operator acceptance

Commands below use the tracked seam fixture for fast validation. Use a larger
representative extract for meaningful resource and interruption measurements.

```bash
set -eu
BIN="$PWD/bin/pathcraft"
FIXTURE="$PWD/pkg/plugins/worldgraph/builder/testdata/seam.osm.pbf"
ROOT="$(mktemp -d)"
STORE_A="$ROOT/store-a"
STORE_B="$ROOT/store-b"
WORK_A="$ROOT/work-a"
WORK_B="$ROOT/work-b"

make build
TIME_BIN="$(command -v gtime || type -P time)"
"$TIME_BIN" -v "$BIN" chunks build-global \
  --pbf "$FIXTURE" --store "$STORE_A" --work-dir "$WORK_A" \
  --run-memory-mb 16 --pack-mb 1 --open-shards 2 \
  2>"$ROOT/time.txt" | tee "$ROOT/build-a.log"
grep 'Maximum resident set size' "$ROOT/time.txt"
```

Inspect packed-manifest invariants:

```bash
python3 - "$STORE_A/manifest.json" <<'PY'
import json, sys
manifest = json.load(open(sys.argv[1], encoding="utf-8"))
assert manifest["layout"] == "packed"
assert manifest["zoom"] == 12 and manifest["shard_zoom"] == 8
assert manifest.get("tiles", []) == []
assert manifest["regions"][0].get("tiles", []) == []
assert manifest["shards"] == sorted(
    manifest["shards"], key=lambda tile: (tile["Z"], tile["X"], tile["Y"])
)
print(manifest["generation"], len(manifest["shards"]))
PY
```

Build cleanly into another store and compare deterministic generation and
packed bytes. `built_at` can differ, so compare generation artifacts rather
than whole manifest JSON:

```bash
"$BIN" chunks build-global \
  --pbf "$FIXTURE" --store "$STORE_B" --work-dir "$WORK_B" \
  --run-memory-mb 16 --pack-mb 1 --open-shards 2
GEN_A="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["generation"])' "$STORE_A/manifest.json")"
GEN_B="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["generation"])' "$STORE_B/manifest.json")"
test "$GEN_A" = "$GEN_B"
(cd "$STORE_A/generations/$GEN_A" && find shards -type f -print0 | sort -z | xargs -0 sha256sum) >"$ROOT/a.sha256"
(cd "$STORE_B/generations/$GEN_B" && find shards -type f -print0 | sort -z | xargs -0 sha256sum) >"$ROOT/b.sha256"
cmp "$ROOT/a.sha256" "$ROOT/b.sha256"
```

For resume acceptance, start a larger build, interrupt it with `Ctrl-C` after at
least one completed stage or packed shard, then rerun the identical command.
Confirm earlier completed stages are not repeated and final hashes match a
clean build using the comparison above.

Check local routing and HTTP viewport rendering:

```bash
"$BIN" route --chunks "$STORE_A" --mode walk \
  --from-position '12.5683,55.6761' --to-position '12.5685,55.6762'
"$BIN" serve --chunks "$STORE_A" --addr 127.0.0.1:18080 &
SERVER_PID=$!
trap 'kill "$SERVER_PID" 2>/dev/null || true' EXIT
curl -fsS http://127.0.0.1:18080/config | python3 -m json.tool
curl -fsS 'http://127.0.0.1:18080/mode-route?mode=walk&from=12.5683%2C55.6761&to=12.5685%2C55.6762' | python3 -m json.tool
```

Open `http://127.0.0.1:18080/`, enable street-graph rendering, and verify chunk
requests stay bounded while panning. Keep OpenStreetMap attribution visible.
