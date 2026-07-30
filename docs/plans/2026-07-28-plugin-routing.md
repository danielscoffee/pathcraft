# Plugin-First Routing Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Consolidate PathCraft's public plugin system under `pkg/plugins`, make routing modes registry-driven and N-dimensional, and ship a registered 3D air-routing example.

**Architecture:** Keep generic values/interfaces in `pkg/pathcraft/core`; move registry and built-in implementations to `pkg/plugins`. Add a high-level `core.Mode` capability whose request/result types use opaque N-dimensional positions and generic route segments. HTTP and web become registry consumers; built-in modes adapt existing engine primitives.

**Tech Stack:** Go 1.25+, React 19, TypeScript 6, Vitest, standard `net/http` and JSON.

---

### Task 1: Consolidate registry package

**Files:**
- Move: `pkg/pathcraft/registry/registry.go` → `pkg/plugins/registry.go`
- Move: `pkg/pathcraft/registry/registry_test.go` → `pkg/plugins/registry_test.go`
- Modify: all Go imports of `pkg/pathcraft/registry`
- Test: `pkg/plugins/registry_test.go`

**Steps:**
1. Move registry tests first and update package/imports; run `go test ./pkg/plugins` and observe missing APIs.
2. Move registry implementation into existing `pkg/plugins` package beside nearest-node index.
3. Update imports to `github.com/danielscoffee/pathcraft/pkg/plugins`.
4. Run `go test ./pkg/plugins ./pkg/pathcraft/engine ./internal/cli`.
5. Commit: `refactor(plugins): consolidate public registry`.

### Task 2: Move standard plugin implementations

**Files:**
- Move: `pkg/pathcraft/plugins/*` → `pkg/plugins/*`
- Modify: `cmd/pathcraft/main.go`
- Modify: docs and Go imports referencing old implementation paths
- Test: `pkg/plugins/plugins_e2e_test.go`

**Steps:**
1. Move end-to-end test and make it import new paths; run it to confirm missing packages.
2. Move adapter and implementation packages without changing behavior.
3. Update blank imports and internal cross-imports.
4. Remove empty old directories.
5. Run `go test ./pkg/plugins/... ./cmd/pathcraft`.
6. Commit: `refactor(plugins): move standard plugins under pkg`.

### Task 3: Add generic routing-mode contract

**Files:**
- Create: `pkg/pathcraft/core/mode.go`
- Modify: `pkg/plugins/registry.go`
- Modify: `pkg/plugins/registry_test.go`

**Steps:**
1. Add failing registry tests for registering, finding, duplicate-rejecting, and sorting `core.Mode` values.
2. Run `go test ./pkg/plugins` and confirm missing mode APIs.
3. Add N-dimensional `Position`, manifest/option, request, segment, result, and `Mode` types to core.
4. Add mode storage and methods to registry.
5. Run `go test ./pkg/pathcraft/core ./pkg/plugins`.
6. Commit: `feat(plugins): add generic routing mode contract`.

### Task 4: Add standard street and GTFS modes

**Files:**
- Create: `pkg/plugins/walk/walk.go`, `walk_test.go`
- Create: `pkg/plugins/bike/bike.go`, `bike_test.go`
- Create: `pkg/plugins/car/car.go`, `car_test.go`
- Create: `pkg/plugins/gtfsmode/gtfs.go`, `gtfs_test.go`
- Create if needed: `pkg/plugins/modetest/engine.go`
- Modify: `cmd/pathcraft/main.go`

**Steps:**
1. Write failing tests against small OSM/GTFS fixtures for manifests and generic segments.
2. Add minimal plugins that assert the required public engine host and adapt existing engine results.
3. Register each plugin from `init()` and blank-import it from the app.
4. Keep routing calculations in existing engine/internal implementations; plugins only select policy and normalize results.
5. Run package tests and engine tests.
6. Commit: `feat(plugins): add standard routing modes`.

### Task 5: Add 3D air mode

**Files:**
- Create: `pkg/plugins/air/air.go`
- Create: `pkg/plugins/air/air_test.go`
- Modify: `cmd/pathcraft/main.go`

**Steps:**
1. Write a failing test with two geographic 3D positions and assert output retains three coordinates per point, includes cruise altitude, and reports positive distance/duration.
2. Implement direct air routing with stdlib math and configurable `cruise_altitude_m` / `speed_mps` defaults.
3. Register from `init()` and activate from app imports.
4. Run `go test ./pkg/plugins/air ./pkg/plugins`.
5. Commit: `feat(plugins): add three-dimensional air routing`.

### Task 6: Add registry-driven HTTP API

**Files:**
- Modify: `internal/http/types.go`
- Modify: `internal/http/server.go`
- Create: `internal/http/handlers_modes.go`
- Modify: `internal/http/router_test.go`

**Steps:**
1. Add failing tests proving `/modes` comes from an isolated registry and `/mode-route` dispatches a fake N-dimensional mode.
2. Add `NewServerWithRegistry`; keep `NewServer` using `plugins.Default`.
3. Generate `/modes` from sorted manifests.
4. Parse comma-separated N-dimensional `from`/`to`; pass remaining query values as options; dispatch plugin with engine as host.
5. Encode generic result and map plugin/input errors to stable HTTP statuses.
6. Add air HTTP test asserting altitude survives JSON.
7. Run `go test ./internal/http`.
8. Commit: `feat(http): dispatch registered routing modes`.

### Task 7: Make web generic mode consumer

**Files:**
- Modify: `web/app/src/api/types.ts`
- Modify: `web/app/src/api/client.ts`
- Modify: `web/app/src/hooks/useRouting.ts`
- Modify: `web/app/src/components/ModeTabs.tsx`
- Modify: `web/app/src/components/RouteSummary.tsx`
- Modify: `web/app/src/components/MapView.tsx`
- Modify: `web/app/src/lib/mapStyle.ts`
- Modify: `web/app/src/lib/modeIcons.ts`
- Create: `web/app/src/lib/modeResult.ts`
- Create: `web/app/src/lib/modeResult.test.ts`
- Modify/remove: mode-specific speed tests and helpers

**Steps:**
1. Add failing tests for converting generic N-dimensional segments to GeoJSON and reading departure-time requirements from manifest options.
2. Mirror mode manifests/results in TypeScript.
3. Replace endpoint/kind request branching with one generic client call.
4. Remove frontend mode speed estimates; consume duration/distance from result.
5. Use manifest colors/icons and segment presentation fields; keep safe fallback icon/color.
6. Drive departure-time input from option descriptors; render generic segment itinerary metadata.
7. Run `npm test`, `npm run lint`, and `npm run build` in `web/app`.
8. Commit: `refactor(web): consume registered routing modes`.

### Task 8: Documentation, compatibility audit, and verification

**Files:**
- Modify: `README.md`
- Modify: `docs/architecture/plugin-system.md`
- Modify: `docs/architecture/overview.md`
- Modify: `docs/architecture/current-system.md`
- Modify: package/import examples across docs

**Steps:**
1. Update package paths and document `core.Mode`, registration, generic positions, and air example.
2. Search for old package imports and hardcoded mode catalogs/profile switches; remove implementation ownership from adapters where in scope.
3. Run `gofmt` on changed Go files.
4. Run `go test ./...` and `go vet ./...`.
5. Run frontend tests, lint, and production build.
6. Run diagnostics on all changed source files.
7. Build `pathcraft`, start server on an ephemeral local port, call `/modes`, and route `air` with 3D endpoints.
8. Use one fresh read-only architecture/correctness reviewer; route fixes to the original writer; repeat validation once.
9. Commit: `docs: document plugin-first routing` and any scoped review fix commit.
