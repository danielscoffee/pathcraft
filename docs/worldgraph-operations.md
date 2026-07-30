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

Global routing zoom is fixed at 12; `--zoom` exists only to reject accidental
other values. Resource flags mean:

| Flag | Default | Effect |
|---|---:|---|
| `--run-memory-mb` | 512 | Maximum records held by each external-sort run; not a process RSS limit |
| `--pack-mb` | 1024 | Maximum size of each immutable pack segment, not total store size |
| `--open-shards` | 64 | Maximum contribution spool descriptors kept open |
| `--resume` | true | Reuse matching completed stages and packed shards |

Progress names each stage and ends with generation, shard, chunk, way,
reference, node, edge, contribution, work-byte, store-byte, and elapsed totals.

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
3. external reference sort;
4. referenced-node selection;
5. contribution partitioning;
6. per-shard pack writing;
7. atomic publication.

Per-shard completion is checkpointed. A resumed pack stage skips already synced
shards instead of rebuilding the whole snapshot.

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

## Capacity planning

Work space can exceed final store size because it simultaneously holds raw and
sorted references, way spools, the compact node index, contribution spools, and
a staged generation. Publication can also coexist with all previously retained
generations. PBF compression ratios and road density vary too much for one safe
source-size multiplier.

Before a planet build:

1. run a representative extract with production resource flags;
2. record printed `Work bytes`, `Store bytes`, and peak RSS;
3. extrapolate with headroom for denser regions and sort runs;
4. reserve room for work data, staged output, current output, and retained old
   generations at the same time;
5. monitor free bytes and inode counts during the build.

`--run-memory-mb` controls sort-run payloads only. Decoder buffers, indexes,
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
