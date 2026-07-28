# Pathcraft Repository Audit

Date: 2026-07-27

## Verdict

Phases 0.1–0.3 now match repository behavior. Pathcraft exposes configurable street routing, a runnable HTTP prototype with opt-in CORS, and timetable-aware walk + transit journeys. It remains a prototype rather than a production routing service.

## What Was Verified

- Focused engine and A* tests cover mode defaults, speed, access restrictions, and highway penalties.
- HTTP tests cover disabled-by-default CORS, exact allowlists, preflight, and timed journey responses.
- RAPTOR and engine tests prove access-walk timing changes trip eligibility and reconstruct scheduled leg times.
- A*, RAPTOR, and public-engine benchmarks run with standard Go benchmark tooling.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go build ./...` pass.
- Frontend tests, lint, and production build pass.

## Current Capability Snapshot

### Working now

- OSM parsing and mode-aware graph construction
- configurable walk, bike, and car defaults through `engine.Config`
- highway penalty multipliers that affect A* route selection
- coordinate and node-ID street routing
- GTFS ingestion and RAPTOR transit routing
- time-dependent walk → transit → walk journeys
- scheduled departure, arrival, and duration on journey legs
- CLI, JSON/GeoJSON HTTP API, and embedded map viewer
- disabled-by-default exact-origin CORS allowlists
- routing and ingestion benchmark suites

### Still prototype-grade

- multimodal stop access uses nearest-stop candidates
- GTFS calendar/service-day filtering is not applied
- no GTFS-realtime, traffic, turn instructions, geocoding, auth, or rate limiting
- HTTP contracts remain debug/demo oriented
- no packaged release, Docker image, hosted demo, or horizontal scaling story

## Roadmap vs Code Reality

### Phase 0.1 – Engine stabilization

Complete for current prototype scope: public facade, configuration, deterministic tests, route export, memory layout work, and benchmarks exist. `New()` remains compatible; `NewWithConfig` adds validated defaults and penalties.

### Phase 0.2 – HTTP server

Complete for prototype scope: server aliases, route/health endpoints, JSON and GeoJSON responses, SPA, and opt-in CORS exist. CORS supports exact origins and read-only preflight; it does not imply production API security.

### Phase 0.3 – Transit routing

Complete for timetable routing scope: GTFS, RAPTOR, access/egress walking, scheduled trip selection, and timed journey legs exist. Calendar dates and realtime updates remain explicit later work.

## Remaining Highest-Value Work

1. Apply GTFS service calendars, then ingest GTFS-realtime.
2. Improve stop access search and expose explicit waiting legs.
3. Add address/geocoder discovery.
4. Add production API controls and packaging.
5. Benchmark additional city-scale fixtures before performance claims.
