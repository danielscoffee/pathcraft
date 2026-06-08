# Pathcraft Repository Audit

Date: 2026-03-08

## Verdict

Pathcraft is going in the right direction.

The repo is now more coherent than before: the docs, CLI, engine, and HTTP surface line up better, coordinate-based walking routes are implemented, and there is now an initial walk + transit flow instead of only separate walking and transit demos.

It is still a prototype, not a polished routing platform, but it is now a more believable and usable prototype.

## What Was Verified

- `go test ./...` passes.
- `go build ./...` passes.
- `go run ./cmd/pathcraft route --file examples/example.osm --from-lat -8.05428 --from-lon -34.88130 --to-lat -8.05480 --to-lon -34.88030 --coords` works.
- `go run ./cmd/pathcraft journey --file examples/example.osm --gtfs examples/mini_gtfs --from-lat -8.05428 --from-lon -34.88130 --to-lat -8.05480 --to-lon -34.88030 --time 05:00:00` works.
- `GET /health` works.
- `GET /route` now works with node IDs and with coordinates.
- `GET /journey` works when GTFS stop coordinates are loaded.

## What Improved

- `pathcraft serve` and `pathcraft server` are both valid.
- `/health` exists in addition to `/status`.
- The engine now exposes coordinate-based walking routing.
- The engine now exposes an initial multimodal route search that compares direct walking with walk + transit.
- GTFS loading can now include `stops.txt`, enabling stop-coordinate-based access and egress legs.
- The README and roadmap are much closer to the real product surface.

## Current Capability Snapshot

### Working well now

- OSM parsing and graph construction
- A* walking routes
- RAPTOR transit routing
- coordinate-based walking route lookup via nearest graph nodes
- initial walk + transit journey planning via nearest GTFS stops
- CLI demos for walking, transit, and multimodal journeys
- HTTP debug endpoints for walking and multimodal queries

### Still prototype-grade

- multimodal routing uses nearest-stop heuristics rather than a deeper integrated street-transit model
- the HTTP API is still query-based and debug-oriented
- the map viewer is still hardcoded around a demo center
- stop discovery and ergonomics are still thin for real users
- there is still no Docker packaging, CORS support, or benchmark suite

## Roadmap vs Code Reality

### Now accurate or mostly accurate

- Phase 0 walking work is implemented.
- Phase 0.2 server work is implemented as a runnable prototype.
- Phase 0.3 transit routing is implemented.
- Walk + Transit integration is now implemented in an initial form.

### Still overstated or unfinished

- The config struct item is still not implemented.
- Time-dependent multimodal routing is still not implemented.
- Docker-ready server packaging is still absent.
- Performance/scale roadmap items are still future work.

## Usability Readout

### Better than before

- Walking CLI no longer requires only raw node IDs.
- HTTP routing no longer requires only raw node IDs.
- There is now a concrete multimodal example dataset in `examples/mini_gtfs`.
- The engine is more useful as a library because it now supports routing by coordinates and a basic multimodal flow.

### Still awkward

- Multimodal journeys depend on GTFS stop coordinates and a graph that roughly covers those stops.
- The journey response is useful, but still minimal.
- Users still need exact GTFS stop IDs for direct transit-only queries.

## Remaining Highest-Value Fixes

### 1. Improve multimodal quality

- replace simple nearest-stop candidate selection with better stop access search
- include explicit wait times and richer transit leg metadata
- add constraints and ranking beyond earliest arrival only

### 2. Improve API usability

- add stop listing and stop search helpers
- make HTTP response contracts more product-like and less debug-like
- add CORS if browser clients are expected

### 3. Improve map and frontend usability

- derive map bounds from graph data
- let the viewer call the new coordinate-based route and journey APIs directly
- visualize journey legs with distinct styles

### 4. Harden the codebase

- add more tests around CLI and full engine flows
- add benchmarks for A* and RAPTOR
- resolve known model issues like ignored OSM one-way handling and unused edge cost fields

## Recommendation

The repo no longer needs a basic coherence rescue first; that was the right move and it helped.

The next good move is to harden the new multimodal path instead of jumping immediately to scale features:

1. improve stop selection and journey leg detail,
2. improve stop/user discovery UX,
3. then expand performance and deployment work.

That sequence keeps Pathcraft moving from “solid algorithm prototype” toward “usable routing engine prototype” without losing focus.
