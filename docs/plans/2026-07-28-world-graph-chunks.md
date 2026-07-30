# Worldwide Graph Chunks Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Build regional OSM PBF files into versioned XYZ graph stores, route existing street modes transparently across loaded chunks, and render viewport chunks through the existing Streets layer.

**Architecture:** `pkg/plugins/worldgraph` owns tile math, immutable chunk generations, bounded cache, corridor expansion, rendering, and a registered graph loader. Build-time PBF decoding uses a temporary bbolt node index. Runtime constructs request-local graph unions and reuses shared context-aware A* primitives; no mutable global graph is assembled.

**Tech Stack:** Go 1.25.12, `github.com/paulmach/osm v0.9.0`, `go.etcd.io/bbolt v1.5.0`, existing protobuf/zlib stack, React 19, Leaflet, Vitest.

**Dependency evidence:** `paulmach/osm` exposes a context-aware streaming PBF scanner with element filters and parallel block decoding ([source](https://github.com/paulmach/osm/blob/e4a5a9962c792a3206f9ff2b45094011f5b5e8f2/osmpbf/scanner.go#L11-L59)). bbolt provides a pure-Go file-backed KV store with bounded lock waiting through `Options.Timeout` ([source](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/db.go#L178-L253), [options](https://github.com/etcd-io/bbolt/blob/e7a8b2dd498494a3766ba24dd94d3509e5588485/db.go#L1322-L1360)).

---

### Task 1: Pin dependencies and add XYZ tile primitives

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `pkg/plugins/worldgraph/tile.go`
- Create: `pkg/plugins/worldgraph/tile_test.go`

**Step 1: Write failing tile tests**

Cover:

```go
func TestTileForPosition(t *testing.T)
func TestTileForPositionRejectsMercatorOverflow(t *testing.T)
func TestTileBoundsRoundTrip(t *testing.T)
func TestWrappedCorridorUsesShortestAntimeridianSpan(t *testing.T)
func TestTileRingClampsYAndWrapsX(t *testing.T)
```

Required API:

```go
const DefaultZoom = 12
const MaxMercatorLatitude = 85.05112878

type TileID struct { Z, X, Y int }
func TileForPosition(lon, lat float64, zoom int) (TileID, error)
func (id TileID) Bounds() Bounds
func Corridor(from, to TileID, halo int) ([]TileID, error)
func Expand(tiles []TileID, rings, maxTiles int) ([]TileID, error)
```

**Step 2: Verify RED**

Run:

```bash
go test ./pkg/plugins/worldgraph -run 'TestTile|TestWrapped' -v
```

Expected: build failure because tile API does not exist.

**Step 3: Implement minimal Web Mercator math**

Use standard-library `math`; reject NaN/infinity and latitude outside Web Mercator bounds. Sort returned IDs by `Z`, `X`, `Y` for deterministic builds/tests.

**Step 4: Pin reviewed dependencies**

```bash
go get github.com/paulmach/osm@v0.9.0
go get go.etcd.io/bbolt@v1.5.0
go mod tidy
```

**Step 5: Verify GREEN**

```bash
go test ./pkg/plugins/worldgraph -v
go vet ./pkg/plugins/worldgraph
```

**Step 6: Commit**

```bash
git add go.mod go.sum pkg/plugins/worldgraph/tile.go pkg/plugins/worldgraph/tile_test.go
git commit -m "feat(worldgraph): add XYZ tile primitives"
```

---

### Task 2: Share OSM street-access policy

**Files:**
- Create: `internal/osm/policy.go`
- Create: `internal/osm/policy_test.go`
- Modify: `internal/osm/filter.go`
- Modify: `internal/osm/graph_builder.go`
- Modify: existing OSM tests as required

**Step 1: Write failing policy tests**

Define one exported internal policy used by XML and PBF paths:

```go
type Direction uint8
const (
    DirectionBoth Direction = iota
    DirectionForward
    DirectionReverse
)

type WayPolicy struct {
    Routable          bool
    Direction         Direction
    RestrictWalking   bool
    RestrictDriving   bool
}

func PolicyForTags(tags map[string]string) WayPolicy
```

Tests must cover residential, footway, motorway, private access, foot/car overrides, service restrictions, `oneway=yes`, `oneway=-1`, and roundabout defaults.

**Step 2: Verify RED**

```bash
go test ./internal/osm -run TestPolicyForTags -v
```

Expected: undefined API.

**Step 3: Implement policy by moving existing decisions, not rewriting them**

`Filter.IsWalkable`, `drivingDirection`, `drivingRestricted`, and `walkingRestricted` must delegate to `PolicyForTags`. Preserve existing behavior.

**Step 4: Verify GREEN and regression suite**

```bash
go test ./internal/osm ./internal/routing/astar ./pkg/pathcraft/engine
```

**Step 5: Commit**

```bash
git add internal/osm
git commit -m "refactor(osm): share street way policy"
```

---

### Task 3: Add chunk model, manifest, codec, and immutable store

**Files:**
- Create: `pkg/plugins/worldgraph/chunk.go`
- Create: `pkg/plugins/worldgraph/manifest.go`
- Create: `pkg/plugins/worldgraph/codec.go`
- Create: `pkg/plugins/worldgraph/store.go`
- Create: `pkg/plugins/worldgraph/store_test.go`
- Create: `pkg/plugins/worldgraph/errors.go`

**Step 1: Write failing store tests**

Cover:

```go
func TestChunkRoundTrip(t *testing.T)
func TestChunkRejectsChecksumMismatch(t *testing.T)
func TestChunkRejectsUnsupportedVersion(t *testing.T)
func TestStoreDistinguishesUncoveredMissingAndCorrupt(t *testing.T)
func TestPublishGenerationPreservesPreviousManifestOnFailure(t *testing.T)
func TestPublishGenerationReusesUnchangedChunks(t *testing.T)
```

Model:

```go
type Node struct {
    ID int64
    Lon, Lat float64
    Owner TileID
}

type EdgeID struct { WayID, From, To int64 }

type Edge struct {
    ID EdgeID
    DistanceMeters float64
    Highway, Name string
    RestrictWalking, RestrictDriving bool
    Owner TileID
    Sources []string
}

type Chunk struct {
    Tile TileID
    Nodes []Node
    Edges []Edge
}
```

Manifest must include format version, preprocessing version, generation, zoom, regions, source hashes, and sorted tile membership.

**Step 2: Verify RED**

```bash
go test ./pkg/plugins/worldgraph -run 'TestChunk|TestStore|TestPublish' -v
```

**Step 3: Implement codec and store**

Use:

- fixed magic and version header;
- SHA-256 payload checksum;
- `encoding/gob` payload for trusted local artifacts;
- structural validation before returning a chunk;
- file mode `0600` by default;
- immutable `generations/<generation>/<z>/<x>/<y>.pcg` directories;
- atomically renamed `manifest.json` as current-generation pointer.

Inject file operations only through unexported test seams. Do not expose arbitrary rename hooks publicly.

**Step 4: Verify GREEN**

```bash
go test ./pkg/plugins/worldgraph -run 'TestChunk|TestStore|TestPublish' -v
go test -race ./pkg/plugins/worldgraph
```

**Step 5: Commit**

```bash
git add pkg/plugins/worldgraph
git commit -m "feat(worldgraph): add versioned chunk store"
```

---

### Task 4: Add disk-backed node index and PBF object scanner

**Files:**
- Create: `pkg/plugins/worldgraph/builder/index.go`
- Create: `pkg/plugins/worldgraph/builder/index_test.go`
- Create: `pkg/plugins/worldgraph/builder/scanner.go`
- Create: `pkg/plugins/worldgraph/builder/scanner_test.go`
- Create: `pkg/plugins/worldgraph/builder/testdata/seam.osm.pbf`
- Create: `scripts/generate-worldgraph-fixture.go`

**Step 1: Write failing index tests**

API:

```go
type NodeIndex struct { /* bbolt DB */ }
func OpenNodeIndex(path string) (*NodeIndex, error)
func (i *NodeIndex) PutBatch(ctx context.Context, nodes []worldgraph.Node) error
func (i *NodeIndex) Get(id int64) (worldgraph.Node, bool, error)
func (i *NodeIndex) Close() error
```

Test ordered signed-ID encoding, batch writes, absent IDs, cancellation, `0600` mode, and lock timeout.

**Step 2: Verify RED**

```bash
go test ./pkg/plugins/worldgraph/builder -run TestNodeIndex -v
```

**Step 3: Implement minimal bbolt index**

Use big-endian sortable keys, fixed-width coordinate values, bounded one-second open timeout, and batched transactions. Never retain bbolt value slices outside transaction lifetime.

**Step 4: Write failing scanner tests**

Scanner API must permit two passes over one file:

```go
func ScanNodes(ctx context.Context, path string, consume func([]worldgraph.Node) error) error
func ScanWays(ctx context.Context, path string, consume func(Way) error) error
```

Test tiny valid fixture, malformed/truncated PBF, cancellation, coordinate rejection, and excessive way-node count.

**Step 5: Generate and document tiny fixture**

Generator creates deterministic PBF crossing one zoom-12 seam. Commit generator and output; include provenance comment and SHA-256 assertion in test.

**Step 6: Implement scanner with `paulmach/osm/osmpbf`**

Use context, `Skip*` flags, bounded worker count, and copied values. Do not store library-owned/reused buffers.

**Step 7: Verify GREEN**

```bash
go test ./pkg/plugins/worldgraph/builder -v
go test -race ./pkg/plugins/worldgraph/builder
```

**Step 8: Commit**

```bash
git add pkg/plugins/worldgraph/builder scripts/generate-worldgraph-fixture.go
git commit -m "feat(worldgraph): stream OSM PBF inputs"
```

---

### Task 5: Build, merge, and atomically publish regional generations

**Files:**
- Create: `pkg/plugins/worldgraph/builder/builder.go`
- Create: `pkg/plugins/worldgraph/builder/contributions.go`
- Create: `pkg/plugins/worldgraph/builder/builder_test.go`
- Modify: `pkg/plugins/worldgraph/manifest.go`
- Modify: `pkg/plugins/worldgraph/store.go`

**Step 1: Write failing build tests**

Cover:

```go
func TestBuildAssignsOneOwnerAndNeighborHalos(t *testing.T)
func TestBuildMergesOverlappingRegionsByStableEdgeID(t *testing.T)
func TestReplacingRegionRemovesDeletedEdges(t *testing.T)
func TestReplacingRegionPreservesSharedBorderEdges(t *testing.T)
func TestCancelledBuildLeavesCurrentGenerationUntouched(t *testing.T)
func TestMalformedPBFLeavesCurrentGenerationUntouched(t *testing.T)
```

Public API:

```go
type Options struct {
    PBFPath string
    StorePath string
    Region string
    Zoom int
    TempDir string
    MaxWayNodes int
}
func Build(ctx context.Context, options Options) (worldgraph.Manifest, error)
```

**Step 2: Verify RED**

```bash
go test ./pkg/plugins/worldgraph/builder -run 'TestBuild|TestReplacing|TestCancelled' -v
```

**Step 3: Implement bounded build**

Use temporary bbolt buckets for node lookup and per-tile contributions. Convert PBF tags through `internal/osm.PolicyForTags`. Compute distance with existing haversine helper. Assign one edge owner by midpoint tile; add edge and required endpoint nodes to owner plus seam-neighbor chunks.

For a replacement import:

- read old region tile membership;
- remove that region from matching edge source sets;
- delete edges whose source set becomes empty;
- merge new contribution;
- hard-link unchanged old-generation chunks into staging when possible, otherwise copy;
- write changed chunks;
- fsync files/directories;
- publish manifest last.

**Step 4: Verify GREEN**

```bash
go test ./pkg/plugins/worldgraph/builder -v
go test -race ./pkg/plugins/worldgraph/builder
```

**Step 5: Commit**

```bash
git add pkg/plugins/worldgraph
git commit -m "feat(worldgraph): build regional chunk generations"
```

---

### Task 6: Add cancellation-aware shared street routing

**Files:**
- Modify: `internal/routing/astar/astar.go`
- Modify: `internal/routing/astar/astar_test.go`
- Create: `internal/routing/street/route.go`
- Create: `internal/routing/street/route_test.go`
- Modify: `pkg/pathcraft/engine/routing.go`
- Modify: `pkg/pathcraft/engine/engine_test.go`

**Step 1: Write failing cancellation test**

Required API:

```go
func AStarWithProfileContext(
    ctx context.Context,
    g *graph.Graph,
    source, target graph.NodeID,
    h geo.Heuristic,
    profile mobility.Profile,
) (*Path, error)
```

Cancel context before call and during a large search; require `context.Canceled` via `errors.Is`.

**Step 2: Verify RED**

```bash
go test ./internal/routing/astar -run TestAStarWithProfileContext -v
```

**Step 3: Implement context checks**

Keep current `AStarWithProfile` as `context.Background()` wrapper. Check context before search and periodically in expansion loop, not on every edge.

**Step 4: Write failing shared street-route tests**

Move graph snapping/path/result construction behind:

```go
package street

type Request struct {
    FromLat, FromLon, ToLat, ToLon float64
    Profile mobility.Profile
    IncludeCoordinates bool
}

type Result struct { /* node IDs, coordinates, distance, duration, snap metadata */ }

func Route(ctx context.Context, g *graph.Graph, request Request) (*Result, error)
```

Test parity with current engine coordinate routes, restrictions, non-finite inputs, and cancellation.

**Step 5: Implement and delegate engine methods**

Add `Engine.RouteByCoordinatesContext`; retain existing method as background-context wrapper. Do not duplicate A* logic.

**Step 6: Verify GREEN**

```bash
go test ./internal/routing/astar ./internal/routing/street ./pkg/pathcraft/engine
go test -race ./internal/routing/astar ./internal/routing/street ./pkg/pathcraft/engine
```

**Step 7: Commit**

```bash
git add internal/routing pkg/pathcraft/engine
git commit -m "refactor(engine): share cancellable street routing"
```

---

### Task 7: Add decoded LRU cache and corridor router

**Files:**
- Create: `pkg/plugins/worldgraph/cache.go`
- Create: `pkg/plugins/worldgraph/cache_test.go`
- Create: `pkg/plugins/worldgraph/router.go`
- Create: `pkg/plugins/worldgraph/router_test.go`
- Create: `pkg/plugins/worldgraph/render.go`

**Step 1: Write failing cache tests**

Cover byte-budget eviction, active-reference safety, one shared concurrent load, failure retry, corruption propagation, and race safety.

API:

```go
type CacheOptions struct { MaxBytes int64 }
type RouterOptions struct {
    CacheBytes int64
    MaxTiles int
    MaxExpansions int
}
func OpenRouter(storePath string, options RouterOptions) (*Router, error)
func (r *Router) Close() error
```

**Step 2: Verify RED**

```bash
go test ./pkg/plugins/worldgraph -run TestCache -v
```

**Step 3: Implement smallest mutex-protected LRU**

Use standard-library list/map plus per-key in-flight result; do not add `x/sync` solely for singleflight.

**Step 4: Write failing router tests**

Cover same-tile route, cross-seam route, walking/driving restrictions, one-way edges, corridor expansion, antimeridian corridor, missing coverage, area limit, no path, corrupt chunk, and cancellation.

Router must implement:

```go
func (r *Router) RouteByCoordinatesContext(context.Context, engine.CoordinateRouteRequest) (*engine.CoordinateRouteResult, error)
func (r *Router) NearestPosition(context.Context, lat, lon float64) (id int64, snapLat, snapLon, distance float64, err error)
func (r *Router) ChunkGeoJSON(context.Context, z, x, y int) ([]byte, error)
func (r *Router) ChunkConfig() (generation string, zoom, minRenderZoom int)
```

**Step 5: Implement request-local graph union**

Load immutable chunks through cache, deduplicate nodes and `EdgeID`, convert to internal graph, call shared street router, then expand corridor on only a no-path result. Never expand on missing/corrupt/cancelled errors.

**Step 6: Verify GREEN**

```bash
go test ./pkg/plugins/worldgraph -v
go test -race ./pkg/plugins/worldgraph
```

**Step 7: Commit**

```bash
git add pkg/plugins/worldgraph
git commit -m "feat(worldgraph): route across cached chunks"
```

---

### Task 8: Register loader and wire existing street modes plus CLI builder

**Files:**
- Create: `pkg/plugins/worldgraph/loader.go`
- Create: `pkg/plugins/worldgraph/loader_test.go`
- Modify: `pkg/plugins/internal/modeutil/street.go`
- Modify: `pkg/plugins/walk/walk_test.go`
- Modify: `pkg/plugins/bike/bike_test.go`
- Modify: `pkg/plugins/car/car_test.go`
- Create: `internal/cli/chunks.go`
- Create: `internal/cli/chunks_test.go`
- Modify: `internal/cli/route.go`
- Modify: `internal/cli/route_test.go`
- Modify: `internal/cli/usage.go`
- Modify: `cmd/pathcraft/main.go`
- Modify: `cmd/pathcraft/main_test.go`

**Step 1: Write failing loader and mode-host tests**

`worldgraph.Loader` name is `worldgraph`, registers with `plugins.Default`, and opens a store. Street mode utility must require context-aware host routing and pass cancellation through.

**Step 2: Verify RED**

```bash
go test ./pkg/plugins/worldgraph ./pkg/plugins/walk ./pkg/plugins/bike ./pkg/plugins/car -v
```

**Step 3: Implement loader and host propagation**

Implement `core.GraphLoader` on a lazy world graph. Keep core contracts geography-neutral. Existing engine and world router both satisfy private street-host capability.

**Step 4: Write failing CLI tests**

Commands:

```text
pathcraft chunks build --pbf <file> --store <dir> --region <name> [--zoom 12]
pathcraft route --chunks <dir> --mode walk --from-position <lon,lat> --to-position <lon,lat>
```

Require exactly one of `--file` or `--chunks` for street modes when a graph host is needed. Air remains hostless.

**Step 5: Implement CLI dispatch**

Add `chunks` command with required `build` subcommand. Parse with `flag.ContinueOnError` so tests never exit process. Print generation, tile count, and region summary.

**Step 6: Verify GREEN**

```bash
go test ./cmd/pathcraft ./internal/cli ./pkg/plugins/... -v
```

**Step 7: Commit**

```bash
git add cmd/pathcraft internal/cli pkg/plugins
git commit -m "feat(cli): build and route world chunks"
```

---

### Task 9: Add transparent chunk host to HTTP server

**Files:**
- Modify: `internal/http/types.go`
- Modify: `internal/http/server.go`
- Modify: `internal/http/handlers_config.go`
- Modify: `internal/http/handlers_route.go`
- Create: `internal/http/handlers_chunks.go`
- Create: `internal/http/handlers_chunks_test.go`
- Modify: `internal/http/server_config_test.go`
- Modify: `internal/http/router_test.go`
- Modify: `internal/cli/server.go`
- Modify: `internal/cli/server_test.go`
- Modify: `internal/http/builtin_modes.go`

**Step 1: Write failing host/config tests**

Server must hold `modeHost any` separately from optional legacy engine. Existing constructors set both to engine. Add constructor for explicit host without breaking tests.

`/config` emits `graph_chunks` only when host exposes chunk capability.

**Step 2: Verify RED**

```bash
go test ./internal/http -run 'Test.*Chunk|Test.*Config' -v
```

**Step 3: Implement endpoint and typed error mapping**

Route:

```text
GET /graph/chunks/{generation}/{z}/{x}/{y}
```

Return owned-edge GeoJSON with `Content-Type: application/geo+json`, generation ETag, and immutable cache headers. Map invalid `400`, uncovered `404`, stale generation `409`, missing/corrupt expected chunk `503`.

Update nearest handler to use `NearestPosition(ctx, ...)` when chunk host is active. Legacy engine path remains.

**Step 4: Write failing CLI server tests**

Add `--chunks`, `--chunk-cache-mb`, `--route-max-tiles`, and `--route-max-expansions`. Reject simultaneous `--file` and `--chunks`. Preserve loopback default.

**Step 5: Implement server wiring**

Open and close world router for server lifetime. Existing mode IDs remain unchanged. Do not add `world-walk` aliases.

**Step 6: Verify GREEN**

```bash
go test ./internal/http ./internal/cli -v
go test -race ./internal/http ./internal/cli
```

**Step 7: Commit**

```bash
git add internal/http internal/cli
git commit -m "feat(http): serve routable graph chunks"
```

---

### Task 10: Render viewport chunks in existing Streets layer

**Files:**
- Modify: `web/app/src/api/types.ts`
- Modify: `web/app/src/api/client.ts`
- Modify: `web/app/src/App.tsx`
- Modify: `web/app/src/components/MapView.tsx`
- Modify: `web/app/src/components/overlays.tsx`
- Create: `web/app/src/lib/graphChunks.ts`
- Create: `web/app/src/lib/graphChunks.test.ts`
- Modify/add overlay tests as needed

**Step 1: Write failing pure viewport tests**

API:

```ts
export interface GraphChunkConfig {
  generation: string
  zoom: number
  min_render_zoom: number
  url: string
}

export function visibleChunkIDs(
  bounds: { west: number; south: number; east: number; north: number },
  zoom: number,
): string[]
```

Test ordinary bounds, antimeridian, Mercator clamp, deterministic order, and tile-count cap.

**Step 2: Verify RED**

```bash
cd web/app
npm test -- src/lib/graphChunks.test.ts
```

Expected: missing module/API.

**Step 3: Implement tile math and API client**

Add `fetchGraphChunk(config, id, signal)`. URL substitution must only use validated integer IDs and server-provided generation; never concatenate arbitrary HTML.

**Step 4: Write failing viewport-manager tests**

Extract small state manager or hook covering:

- maximum six in-flight requests;
- abort on disabled/stale generation;
- progressive publication;
- offscreen layer removal;
- 64-tile LRU;
- no requests below minimum zoom.

**Step 5: Replace full graph fetch**

`StreetGraphLayer` listens to `moveend`/`zoomend`, renders one Leaflet GeoJSON layer per visible tile, preserves text-only tooltips, and derives legend from visible layers. If `graph_chunks` is absent, retain current one-shot `/graph` fallback for single-file mode.

**Step 6: Verify GREEN**

```bash
cd web/app
npm test
npm run lint
npm run build
```

**Step 7: Commit**

```bash
git add web/app/src
git commit -m "feat(web): render viewport graph chunks"
```

---

### Task 11: End-to-end seam demo and documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/architecture/current-system.md`
- Modify: `docs/architecture/overview.md`
- Modify: `docs/architecture/plugin-system.md`
- Modify: `docs/tutorials/demo.md`
- Modify: `docs/sdk/go.md`
- Create: `pkg/plugins/worldgraph/worldgraph_e2e_test.go`
- Modify: `Makefile`

**Step 1: Write failing end-to-end test**

Using tiny PBF fixture:

1. build store;
2. reopen router;
3. verify manifest generation;
4. route walk and car across seam;
5. fetch both render chunks;
6. verify each owned edge appears once;
7. replace region with deleted edge and verify route disappears.

**Step 2: Verify RED**

```bash
go test ./pkg/plugins/worldgraph -run TestWorldGraphEndToEnd -v
```

**Step 3: Add demo target and docs**

Document:

```bash
pathcraft chunks build --pbf region.osm.pbf --store world --region demo
pathcraft serve --chunks world --addr 127.0.0.1:8080
```

Document resource defaults, Web Mercator bounds, update behavior, trusted-local-artifact boundary, missing-coverage errors, OSM attribution, explicit network exposure, and MVP distance limits.

Add `make demo-chunks PBF=... REGION=... STORE=...` only if it remains a thin command wrapper.

**Step 4: Verify GREEN**

```bash
go test ./pkg/plugins/worldgraph -run TestWorldGraphEndToEnd -v
git diff --check
```

**Step 5: Commit**

```bash
git add README.md docs Makefile pkg/plugins/worldgraph
git commit -m "docs: document worldwide graph chunks"
```

---

### Task 12: Full validation and bounded security/review loop

**Files:**
- Modify only files required by evidence-backed findings.

**Step 1: Run full Go checks**

```bash
gofmt -w <all changed Go files>
go test ./...
go vet ./...
go test -race ./pkg/plugins/worldgraph/... ./internal/http ./internal/cli ./pkg/pathcraft/engine
```

**Step 2: Run frontend checks**

```bash
cd web/app
npm test
npm run lint
npm run build
npm audit --omit=dev
```

**Step 3: Run supply-chain and build checks**

```bash
govulncheck ./...
GOOS=js GOARCH=wasm go build -o /tmp/pathcraft-worldgraph.wasm ./cmd/pathcraft-wasm
go build -o /tmp/pathcraft-worldgraph ./cmd/pathcraft
git diff --check
```

**Step 4: Run real seam integration**

Build tiny fixture store, start server on loopback, assert:

- `/modes` still exposes `walk`, `bike`, `car`, `gtfs`, `air`;
- `/config` exposes chunk generation;
- chunk endpoints return owned GeoJSON;
- invalid/stale/uncovered/corrupt requests map correctly;
- walk/car routes cross seam;
- Streets frontend production bundle loads.

**Step 5: Run bounded review loop**

One fresh read-only reviewer for architecture/correctness and one security audit focused on PBF trust boundary, path handling, bbolt lifecycle, checksums, resource caps, HTTP cache behavior, and cancellation. Parent remains sole writer. Apply only material findings, rerun focused checks, maximum three rounds.

**Step 6: Commit review fixes**

Use scoped Conventional Commits; do not merge or push.

**Step 7: Final state**

Require clean worktree and report exact commands, outcomes, commit range, reviewer limitations, and residual limits (no continental hierarchy, no runtime downloader, explicit reverse-proxy controls for network exposure).
