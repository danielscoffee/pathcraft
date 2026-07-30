# Global Worldgraph Storage Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Build and serve one local planet-wide fixed-zoom worldgraph generation through sparse immutable shard packs.

**Architecture:** Zoom-12 chunks are grouped under zoom-8 shard indexes and immutable pack segments. Runtime resolves coverage through bounded shard-index caching and reads chunk ranges with `ReadAt`; a restartable multi-pass global PBF builder writes a complete new generation and atomically switches the existing manifest pointer.

**Tech Stack:** Go standard library, existing `osmpbf`, existing bbolt temporary indexes, existing worldgraph chunk codec/cache/router, Vitest frontend checks.

---

## Global invariants

- Keep legacy per-tile stores readable.
- New packed stores always use routing zoom 12 and shard zoom 8.
- Never materialize all zoom-12 tile IDs at runtime.
- Never write one filesystem file per zoom-12 tile.
- Keep one writer per worktree and use TDD for every task.
- Run focused tests before each task commit.
- Do not merge or push from this plan.

### Task 1: Shard addressing and binary index codec

**Files:**
- Create: `pkg/plugins/worldgraph/shard.go`
- Create: `pkg/plugins/worldgraph/shard_index.go`
- Create: `pkg/plugins/worldgraph/shard_index_test.go`
- Modify: `pkg/plugins/worldgraph/errors.go`

**Step 1: Write failing shard math tests**

Test zoom-12 tiles mapping to zoom-8 parent and 0–255 row-major slot, including `(0,0)`, `(4095,4095)`, and antimeridian columns:

```go
func TestPackedShardAddress(t *testing.T) {
    tests := []struct {
        tile TileID
        shard TileID
        slot uint8
    }{
        {TileID{Z: 12, X: 0, Y: 0}, TileID{Z: 8, X: 0, Y: 0}, 0},
        {TileID{Z: 12, X: 15, Y: 15}, TileID{Z: 8, X: 0, Y: 0}, 255},
        {TileID{Z: 12, X: 4095, Y: 4095}, TileID{Z: 8, X: 255, Y: 255}, 255},
    }
    // Assert shardAddress and shardTile round-trip.
}
```

**Step 2: Run test and verify failure**

Run:

```bash
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph -run 'TestPackedShard' -v
```

Expected: FAIL because shard helpers do not exist.

**Step 3: Implement fixed addressing**

Add:

```go
const (
    GlobalRoutingZoom = 12
    PackedShardZoom   = 8
    packedShardWidth  = 1 << (GlobalRoutingZoom - PackedShardZoom)
    packedShardSlots  = packedShardWidth * packedShardWidth
)

type shardAddress struct {
    Shard TileID
    Slot  uint8
}

func packedShardAddress(tile TileID) (shardAddress, error)
func packedShardTile(shard TileID, slot uint8) (TileID, error)
```

Reject any non-zoom-12 child or non-zoom-8 shard with `ErrInvalidChunk` wrapping.

**Step 4: Write failing binary index tests**

Cover deterministic round trip, sorted entries, duplicate slots, wrong magic/version, invalid shard, invalid segment, offset overflow, length above `MaxChunkPayloadBytes` plus chunk header, truncation, trailing bytes, and checksum preservation.

Use:

```go
type shardIndexEntry struct {
    Slot     uint8
    Segment  uint16
    Offset   uint64
    Length   uint32
    SHA256   [32]byte
}

type shardIndex struct {
    Shard   TileID
    Entries []shardIndexEntry
}

func encodeShardIndex(io.Writer, shardIndex) error
func decodeShardIndex(io.Reader) (shardIndex, error)
```

**Step 5: Implement minimal fixed binary codec**

Format:

```text
8 bytes magic "PCGIDX\x00\x00"
4 bytes index version (big endian, value 1)
4 bytes shard zoom
4 bytes shard x
4 bytes shard y
4 bytes entry count (0..256)
entries sorted by slot:
  1 byte slot
  2 bytes segment
  8 bytes offset
  4 bytes length
 32 bytes SHA-256
```

Add `ErrCorruptIndex`. Decode through a bounded reader and reject duplicate or unsorted slots.

**Step 6: Verify and commit**

```bash
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph -run 'TestPackedShard|TestShardIndex' -v
git add pkg/plugins/worldgraph/shard.go pkg/plugins/worldgraph/shard_index.go pkg/plugins/worldgraph/shard_index_test.go pkg/plugins/worldgraph/errors.go
git commit -m "feat(worldgraph): add packed shard indexes"
```

### Task 2: Immutable pack segments

**Files:**
- Create: `pkg/plugins/worldgraph/pack.go`
- Create: `pkg/plugins/worldgraph/pack_test.go`
- Modify: `pkg/plugins/worldgraph/codec.go`

**Step 1: Write failing pack writer tests**

Build three test chunks with a tiny injected segment limit. Assert:

- tiles must arrive in shard-slot order;
- duplicate slots fail;
- segment rollover occurs before the configured limit;
- index offsets and lengths identify exact encoded chunks;
- all pack files are mode `0600`;
- cancellation removes stage files;
- short writes and sync failures return errors.

Keep production default:

```go
const DefaultPackSegmentBytes int64 = 1 << 30
```

Use internal options so tests can set 1 KiB:

```go
type packWriterOptions struct {
    MaxSegmentBytes int64
    CreateFile func(string) (*os.File, error)
}
```

**Step 2: Run tests and verify failure**

```bash
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph -run 'TestPack' -v
```

Expected: FAIL because pack writer does not exist.

**Step 3: Implement pack writer**

Add:

```go
type shardPackWriter struct { /* current segment, offset, entries */ }
func newShardPackWriter(ctx context.Context, dir string, shard TileID, options packWriterOptions) (*shardPackWriter, error)
func (w *shardPackWriter) Append(tile TileID, chunk Chunk) error
func (w *shardPackWriter) Close() (shardIndex, error)
func (w *shardPackWriter) Abort() error
```

Encode each chunk once into a bounded temporary buffer, hash the complete encoded record, roll segments deterministically, write completely, and sync files before closing. Do not add another record envelope.

**Step 4: Write failing random-access tests**

Add tests for correct read, wrong digest, truncated segment, index range outside file, context cancellation, and malformed chunk.

```go
func readPackedChunk(ctx context.Context, dir string, entry shardIndexEntry) (*Chunk, error)
```

Use `io.NewSectionReader`, hash exactly `Length`, compare digest, then call `DecodeChunk`.

**Step 5: Implement and verify**

```bash
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph -run 'TestPack' -v
git add pkg/plugins/worldgraph/pack.go pkg/plugins/worldgraph/pack_test.go pkg/plugins/worldgraph/codec.go
git commit -m "feat(worldgraph): write immutable chunk packs"
```

### Task 3: Packed manifest and backward-compatible store reads

**Files:**
- Modify: `pkg/plugins/worldgraph/manifest.go`
- Modify: `pkg/plugins/worldgraph/store.go`
- Modify: `pkg/plugins/worldgraph/store_test.go`
- Create: `pkg/plugins/worldgraph/shard_cache.go`
- Create: `pkg/plugins/worldgraph/shard_cache_test.go`

**Step 1: Write failing manifest tests**

Extend `Manifest`:

```go
const PackedLayout = "packed"

type Manifest struct {
    // Existing fields unchanged.
    Layout        string   `json:"layout,omitempty"`
    LayoutVersion int      `json:"layout_version,omitempty"`
    ShardZoom     int      `json:"shard_zoom,omitempty"`
    Shards        []TileID `json:"shards,omitempty"`
    SourceBytes   int64    `json:"source_bytes,omitempty"`
}
```

Packed validation requires layout version 1, zoom 12, shard zoom 8, sorted unique zoom-8 shards, exactly one full-snapshot source region, no `Tiles`, and no region tile arrays. Legacy validation remains byte-for-byte compatible.

**Step 2: Implement manifest branching**

Split validation into `validateLegacyManifest` and `validatePackedManifest`. Normalize only the fields belonging to the selected layout. `manifestHasTile` remains legacy-only.

**Step 3: Write failing bounded shard-cache tests**

Add a small LRU keyed by zoom-8 shard. Tests cover one-load sharing, byte/count eviction, all-waiters cancellation, replacement after canceled flight, malformed index propagation, and close behavior. Reuse the proven flight identity pattern from `cache.go`.

```go
type shardIndexCache struct { /* bounded LRU + shared flights */ }
func newShardIndexCache(maxEntries int) *shardIndexCache
func (c *shardIndexCache) Get(ctx context.Context, shard TileID, load func(context.Context) (shardIndex, error)) (shardIndex, error)
```

Default to 1,024 indexes, bounded independently from decoded chunks.

**Step 4: Write failing packed store tests**

Manually create a packed generation using Task 2 helpers. Assert:

- `OpenStore` accepts packed and legacy manifests;
- `Store.Covers(ctx,tile)` consults occupancy without global tile map;
- covered reads return chunks;
- absent slot returns `ErrUncoveredTile`;
- missing pack returns `ErrMissingChunk`;
- digest/index/chunk errors return `ErrCorruptChunk`;
- index loads are cached;
- `Store.Close` closes both caches safely.

**Step 5: Implement layout dispatch**

Add to `Store`:

```go
func (s *Store) Covers(ctx context.Context, tile TileID) (bool, error)
func (s *Store) Close() error
```

`LoadChunkContext` dispatches to existing legacy read or packed index/range read. Keep immutable manifest pointer snapshots. Derive deterministic shard/index/segment paths only from validated integers.

**Step 6: Verify and commit**

```bash
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph -run 'Test(PackedManifest|ShardCache|PackedStore|Store)' -v
git add pkg/plugins/worldgraph/manifest.go pkg/plugins/worldgraph/store.go pkg/plugins/worldgraph/store_test.go pkg/plugins/worldgraph/shard_cache.go pkg/plugins/worldgraph/shard_cache_test.go
git commit -m "feat(worldgraph): read packed global stores"
```

### Task 4: Router coverage without a global tile map

**Files:**
- Modify: `pkg/plugins/worldgraph/router.go`
- Modify: `pkg/plugins/worldgraph/router_test.go`
- Modify: `pkg/plugins/worldgraph/loader.go`

**Step 1: Write failing packed-router tests**

Create a packed fixture spanning a zoom-8 shard boundary. Assert `OpenRouter` does not populate `covered` for packed stores, routes across the boundary, rejects an uncovered corridor tile, renders chunks, and closes store resources.

**Step 2: Implement store-backed coverage**

Remove unconditional manifest tile-map construction. Keep the map only for legacy manifests or remove it entirely and call:

```go
covered, err := r.store.Covers(ctx, tile)
```

Change `coveredTiles` and corridor preflight to propagate coverage errors and cancellation. Do not silently convert corrupt index errors to uncovered.

**Step 3: Verify and commit**

```bash
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph -run 'TestRouter.*Packed|TestRouterRoutes' -v
git add pkg/plugins/worldgraph/router.go pkg/plugins/worldgraph/router_test.go pkg/plugins/worldgraph/loader.go
git commit -m "feat(worldgraph): route through sparse shard coverage"
```

### Task 5: Atomic packed generation publication

**Files:**
- Create: `pkg/plugins/worldgraph/packed_publish.go`
- Create: `pkg/plugins/worldgraph/packed_publish_test.go`
- Modify: `pkg/plugins/worldgraph/store.go`

**Step 1: Write failing publication tests**

Test store-owned resumable generation stages. Cover manifest-last visibility, file/directory sync order, cancellation before commit, commit-wins cancellation, stale two-store parent conflict, cross-process lock contention, root-sync rollback retaining a generation pinned during visibility, resume of the same stage, and no destination overwrite.

**Step 2: Implement staging API**

```go
type PackedGenerationStage struct { /* store, prepared manifest, stage path */ }

func (s *Store) BeginPackedGeneration(manifest Manifest) (*PackedGenerationStage, error)
func (s *PackedGenerationStage) Path() string
func (s *PackedGenerationStage) Commit(ctx context.Context) error
func (s *PackedGenerationStage) Abort() error
```

`BeginPackedGeneration` creates or reopens
`generations/.<generation>.build` without holding the publication lock. Builders
write final pack bytes directly there. `Commit` acquires the existing bbolt root
lock, calls `verifyManifestCurrent`, validates every declared index/segment and
rejects unexpected files/symlinks, syncs the tree, renames the stage, then uses
the existing manifest-temp, rename-last commit point, and pinned-reader-safe
rollback. No generation-wide copy is permitted.

**Step 3: Verify and commit**

```bash
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph -run 'TestPublishPacked' -v
git add pkg/plugins/worldgraph/packed_publish.go pkg/plugins/worldgraph/packed_publish_test.go pkg/plugins/worldgraph/store.go
git commit -m "feat(worldgraph): publish packed generations atomically"
```

### Task 6: External fixed-record sorting and compact node index

**Files:**
- Create: `pkg/plugins/worldgraph/builder/external_sort.go`
- Create: `pkg/plugins/worldgraph/builder/external_sort_test.go`
- Create: `pkg/plugins/worldgraph/builder/global_nodes.go`
- Create: `pkg/plugins/worldgraph/builder/global_nodes_test.go`

**Step 1: Write failing external-sort tests**

Generate unsorted signed `int64` node references with duplicates. Force 3-record runs. Assert deterministic signed-order deduplication, bounded runs, cancellation cleanup, truncated-record rejection, and k-way merge correctness.

```go
func sortUniqueInt64File(ctx context.Context, input, output, runDir string, maxRecords int) error
```

Use fixed 8-byte big-endian sign-flipped keys so lexical and signed order match.

**Step 2: Implement bounded run generation and merge**

Use only standard library: bounded slices, sorted run files, `container/heap` k-way merge, private `0600` files, complete writes, sync, rename, and cleanup. Merge at most 64 runs per pass and recursively merge intermediate runs, so arbitrary input size cannot exhaust file descriptors.

**Step 3: Write failing compact node-index tests**

```go
type globalNodeRecord struct {
    ID int64
    Lon float64
    Lat float64
    Owner worldgraph.TileID
}

type globalNodeIndex struct { file *os.File; count int64 }
func openGlobalNodeIndex(path string) (*globalNodeIndex, error)
func (i *globalNodeIndex) Get(id int64) (worldgraph.Node, bool, error)
```

Use one fixed 40-byte record and binary-search with `ReadAt`. Test first/middle/last, signed IDs, missing IDs, malformed size, invalid coordinates, and concurrent reads.

**Step 4: Implement merge-selected nodes**

Add a streaming merge that consumes monotonic PBF nodes and sorted references, emits only referenced nodes, rejects decreasing node IDs, duplicate conflicting nodes, and missing references.

**Step 5: Verify and commit**

```bash
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph/builder -run 'TestExternalSort|TestGlobalNode' -v
git add pkg/plugins/worldgraph/builder/external_sort.go pkg/plugins/worldgraph/builder/external_sort_test.go pkg/plugins/worldgraph/builder/global_nodes.go pkg/plugins/worldgraph/builder/global_nodes_test.go
git commit -m "feat(worldgraph): add disk-sorted global node index"
```

### Task 7: Stable multi-pass PBF source and way spool

**Files:**
- Modify: `pkg/plugins/worldgraph/builder/pbf_validate.go`
- Modify: `pkg/plugins/worldgraph/builder/scanner.go`
- Create: `pkg/plugins/worldgraph/builder/pbf_source.go`
- Create: `pkg/plugins/worldgraph/builder/pbf_source_test.go`
- Create: `pkg/plugins/worldgraph/builder/way_spool.go`
- Create: `pkg/plugins/worldgraph/builder/way_spool_test.go`

**Step 1: Write failing stable-source tests**

Open one regular file descriptor, validate/hash through a section reader, run repeated scanner passes over the same descriptor, and verify final hash/identity. Test path replacement does not change descriptor bytes, in-place rewrite fails final verification, non-regular input fails, and cancellation closes resources.

```go
type pbfSource struct { /* file, size, stat identity, SHA-256 */ }
func openPBFSource(ctx context.Context, path string, maxWayNodes int) (*pbfSource, error)
func (s *pbfSource) Reader() *io.SectionReader
func (s *pbfSource) Verify(ctx context.Context) error
func (s *pbfSource) Close() error
```

Refactor validation/scanners to reader-based internal functions; retain public path wrappers and regional private-snapshot behavior.

**Step 2: Write failing way-spool tests**

Length-prefix compact way records containing ID, node refs, highway, name, and policy inputs. Enforce current way/tag/string/contribution limits before writing. Test round trip, truncation, excessive lengths, cancellation, and deterministic replay.

**Step 3: Implement first global pass**

Stream ways from `pbfSource.Reader()`, retain only routable ways with at least two refs, append way spool records, and append every referenced node ID to the fixed ref file. Do not resolve coordinates in this pass.

**Step 4: Verify and commit**

```bash
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph/builder -run 'TestPBFSource|TestWaySpool|TestScan' -v
git add pkg/plugins/worldgraph/builder/pbf_validate.go pkg/plugins/worldgraph/builder/scanner.go pkg/plugins/worldgraph/builder/pbf_source.go pkg/plugins/worldgraph/builder/pbf_source_test.go pkg/plugins/worldgraph/builder/way_spool.go pkg/plugins/worldgraph/builder/way_spool_test.go
git commit -m "feat(worldgraph): spool stable global PBF passes"
```

### Task 8: Partition contributions by global shard

**Files:**
- Create: `pkg/plugins/worldgraph/builder/shard_spool.go`
- Create: `pkg/plugins/worldgraph/builder/shard_spool_test.go`
- Modify: `pkg/plugins/worldgraph/builder/builder.go`

**Step 1: Write failing bounded file-LRU tests**

Append contributions for more shards than the open-file limit. Assert at most configured files remain open, reopening appends safely, files are `0600`, shard paths are deterministic, cancellation stops writes, and malformed records are rejected on replay.

```go
type shardSpoolWriter struct { /* LRU of append-only shard files */ }
func newShardSpoolWriter(ctx context.Context, root string, maxOpen int) (*shardSpoolWriter, error)
func (w *shardSpoolWriter) Add(edgeContribution) error
func (w *shardSpoolWriter) Close() error
```

**Step 2: Reuse contribution generation**

Extract current segment contribution logic so regional and global builders share policy, ownership, halo, edge identity, and resource limits. Global replay resolves nodes through `globalNodeIndex` and sends contributions to shard spools instead of one monolithic bbolt database.

**Step 3: Add shard replay tests**

Replay one shard into an ephemeral existing `contributionStore`, assert no tile outside the shard, normalize each chunk, and write through `shardPackWriter`. Force segment rollover and validate index occupancy.

**Step 4: Verify and commit**

```bash
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph/builder -run 'TestShardSpool|TestGlobalContribution' -v
git add pkg/plugins/worldgraph/builder/shard_spool.go pkg/plugins/worldgraph/builder/shard_spool_test.go pkg/plugins/worldgraph/builder/builder.go
git commit -m "feat(worldgraph): partition global graph contributions"
```

### Task 9: Restartable global build orchestration

**Files:**
- Create: `pkg/plugins/worldgraph/builder/global.go`
- Create: `pkg/plugins/worldgraph/builder/global_test.go`
- Create: `pkg/plugins/worldgraph/builder/build_state.go`
- Create: `pkg/plugins/worldgraph/builder/build_state_test.go`

**Step 1: Write failing build-state tests**

Persist JSON state with source absolute path, size, SHA-256, routing/shard zoom, memory/run limits, pack limit, completed stages, and output counts. Require atomic write/sync/rename. Matching state resumes; any source/config mismatch fails unless work directory is empty.

**Step 2: Define global options and progress**

```go
type GlobalOptions struct {
    PBFPath string
    StorePath string
    WorkDir string
    RunMemoryBytes int64
    PackSegmentBytes int64
    MaxOpenShards int
    Resume bool
    Progress func(GlobalProgress)
}

func BuildGlobal(ctx context.Context, options GlobalOptions) (worldgraph.Manifest, error)
```

Defaults: 512 MiB run memory, 1 GiB pack segments, 64 open shard spools, resume true, zoom 12/8 fixed.

**Step 3: Write failing staged integration test**

Use a generated PBF containing separate ways in Copenhagen, Recife, and Tokyo. Cancel after each stage and rerun. Assert completed stages are reused, final source verification occurs, multiple shards publish, manifest omits zoom-12 tiles, final packs are written once into a store-owned generation stage, and the legacy store remains untouched until commit.

**Step 4: Implement orchestration**

Stages:

```text
validate-source
spool-ways-and-refs
sort-refs
select-nodes
partition-contributions
write-packs
publish
```

Each intermediate stage writes to a temporary path, syncs, renames, then records completion. `write-packs` obtains `BeginPackedGeneration` and writes packs directly beneath its stage path; `publish` calls `Commit`. Progress counters never require loading global ID lists.

**Step 5: Verify and commit**

```bash
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph/builder -run 'TestBuildState|TestBuildGlobal' -v
git add pkg/plugins/worldgraph/builder/global.go pkg/plugins/worldgraph/builder/global_test.go pkg/plugins/worldgraph/builder/build_state.go pkg/plugins/worldgraph/builder/build_state_test.go
git commit -m "feat(worldgraph): build restartable global generations"
```

### Task 10: CLI and operational progress

**Files:**
- Modify: `internal/cli/chunks.go`
- Modify: `internal/cli/chunks_test.go`
- Modify: `internal/cli/usage.go`
- Modify: `cmd/pathcraft/main_test.go`

**Step 1: Write failing CLI tests**

Cover required flags, fixed zoom rejection, positive resource options, canceled context, progress output, resume behavior, and successful multi-shard fixture build:

```text
pathcraft chunks build-global --pbf FILE --store DIR --work-dir DIR
```

**Step 2: Implement `build-global` subcommand**

Parse:

```text
--pbf
--store
--work-dir
--run-memory-mb (default 512)
--pack-mb (default 1024)
--open-shards (default 64)
--resume (default true)
```

Print stage, input/output counts, elapsed time, final generation, occupied shards, chunks, nodes, edges, work bytes, and store bytes. Keep existing `chunks build` behavior.

**Step 3: Verify and commit**

```bash
CGO_ENABLED=0 go test ./internal/cli ./cmd/pathcraft -run 'TestCmdChunks|TestRun' -v
git add internal/cli/chunks.go internal/cli/chunks_test.go internal/cli/usage.go cmd/pathcraft/main_test.go
git commit -m "feat(cli): build global packed graph stores"
```

### Task 11: HTTP bootstrap center and packed E2E

**Files:**
- Modify: `pkg/plugins/worldgraph/router.go`
- Modify: `internal/http/handlers_config.go`
- Modify: `internal/http/handlers_config_test.go`
- Modify: `pkg/plugins/worldgraph/worldgraph_e2e_test.go`
- Modify: `web/app/src/lib/graphChunks.test.ts`

**Step 1: Write failing coverage bootstrap tests**

Expose through the existing optional chunk host capability:

```go
ChunkViewport() (centerLat, centerLon float64, zoom int)
```

For packed stores, derive center from the midpoint of occupied shard bounds and choose bootstrap zoom no lower than `min_render_zoom`. Handle antimeridian-wrapped coverage. Legacy chunk stores derive from their sorted tiles. Empty coverage is invalid at publication.

**Step 2: Implement `/config` integration**

Chunk-only servers use `ChunkViewport`; engine-backed servers retain graph-derived center. No hard-coded Recife fallback when chunk capability provides coverage.

**Step 3: Add multi-shard loopback E2E**

Build synthetic global PBF, serve packed store, assert `/config`, direct chunks in three distant locations, six-request-compatible immutable responses, local walk/car routes inside each region, uncovered gaps, stale generation, corruption mapping, and no global tile list in manifest.

**Step 4: Verify and commit**

```bash
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph ./internal/http -run 'Test.*Packed|TestWorldGraph' -v
cd web/app && npm test -- src/lib/graphChunks.test.ts
git add pkg/plugins/worldgraph/router.go internal/http/handlers_config.go internal/http/handlers_config_test.go pkg/plugins/worldgraph/worldgraph_e2e_test.go web/app/src/lib/graphChunks.test.ts
git commit -m "feat(http): bootstrap global chunk viewports"
```

### Task 12: Documentation and operator acceptance

**Files:**
- Modify: `README.md`
- Modify: `docs/architecture/current-system.md`
- Modify: `docs/architecture/overview.md`
- Modify: `docs/tutorials/demo.md`
- Modify: `docs/sdk/go.md`
- Modify: `Makefile`
- Create: `docs/tutorials/global-worldgraph.md`

**Step 1: Document exact resource model**

Document full snapshot requirement, expected large disk/time use without promising fixed planet size, work directory on fast local disk, restart semantics, immutable source requirement, pack/index layout, manual old-generation GC, local-routing cap, and trusted-local artifact boundary.

**Step 2: Add make wrapper**

```make
WORLD_PBF ?=
WORLD_STORE ?= world-global
WORLD_WORK ?= .worldgraph-build

global-chunks:
	@test -n "$(WORLD_PBF)"
	@./bin/pathcraft chunks build-global --pbf "$(WORLD_PBF)" --store "$(WORLD_STORE)" --work-dir "$(WORLD_WORK)"
```

Do not download planet data automatically.

**Step 3: Add manual acceptance checklist**

Record commands for SHA-256, available disk, build/resume, serve, viewport checks on three continents, local routes, metrics, and cleanup. State arbitrary intercontinental routes remain unsupported.

**Step 4: Verify and commit**

```bash
CGO_ENABLED=0 go test ./internal/cli ./pkg/plugins/worldgraph/...
git diff --check
git add README.md Makefile docs/architecture docs/tutorials docs/sdk/go.md
git commit -m "docs: document global worldgraph storage"
```

### Task 13: Full validation and bounded review

**Files:**
- Modify only files required by evidence-backed fixes.

**Step 1: Focused diagnostics**

```bash
# Run primary LSP diagnostics on every changed Go/TypeScript file.
CGO_ENABLED=0 go test ./pkg/plugins/worldgraph/... ./internal/http ./internal/cli ./cmd/pathcraft
CGO_ENABLED=0 go vet ./pkg/plugins/worldgraph/... ./internal/http ./internal/cli ./cmd/pathcraft
```

**Step 2: Race and fuzz checks**

```bash
PATH=/nix/store/yqmfmarywhqadkkvd5w9zbz8lw9pzkyj-pkg-config-0.29.2/bin:$PATH \
PKG_CONFIG_PATH=/nix/store/ydgdz8pf2in3rlb5agwkgr76vfrdf2s5-zlib-1.3.2-dev/share/pkgconfig \
  go test -race ./pkg/plugins/worldgraph/... ./internal/http ./internal/cli ./cmd/pathcraft

CGO_ENABLED=0 go test ./pkg/plugins/worldgraph/builder -run '^$' \
  -fuzz FuzzAcceptedPBFPrimitiveBlocksDoNotPanicDecoder -fuzztime=30s
```

Add focused fuzzers for shard-index decode and pack indexed ranges, then run each for 30 seconds.

**Step 3: Full repository and frontend**

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go vet ./...
cd web/app && npm test && npm run lint && npm run build && npm audit --omit=dev
cd ../..
govulncheck ./...
```

**Step 4: Native/WASM builds and live integration**

```bash
PATH=/nix/store/yqmfmarywhqadkkvd5w9zbz8lw9pzkyj-pkg-config-0.29.2/bin:$PATH \
PKG_CONFIG_PATH=/nix/store/ydgdz8pf2in3rlb5agwkgr76vfrdf2s5-zlib-1.3.2-dev/share/pkgconfig \
  go build -o /tmp/pathcraft-global ./cmd/pathcraft
CGO_ENABLED=0 GOOS=js GOARCH=wasm go build -o /tmp/pathcraft-global.wasm ./cmd/pathcraft-wasm
```

Run the multi-shard synthetic build/server and verify viewport chunks and local routes in all fixture regions.

**Step 5: Read-only reviews**

Request fresh architecture and security reviews. Material focus:

- index offset/length overflow and path safety;
- pack digest and chunk validation ordering;
- bounded memory/file descriptors during build and runtime;
- immutable source TOCTOU protection;
- cancellation and publication commit point;
- stale-writer and pinned-reader semantics;
- no global tile list or generation-wide copy;
- legacy compatibility;
- local-only route limits and network exposure documentation.

Fix Critical/High/Important findings only, with at most three material review/fix rounds.

**Step 6: Final state**

```bash
git diff --check
git status --short
git log --oneline development..HEAD
```

Require clean feature worktree. Report exact validation outcomes, commit range, measured synthetic-build counts, reviewer limitations, residual planet-build operational limits, and no merge/push.
