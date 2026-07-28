# Pathcraft – Roadmap

---

## Phase 0 – Foundation

- [x] Fix lint errors(staticcheck and errcheck)
- [x] OSM parsing
- [x] Graph construction
- [x] A* routing (walking)
- [x] CLI interface
- [x] GeoJSON export
- [x] Basic map visualization

---

## Phase 0.1 – Engine Stabilization

Goal: Make Pathcraft usable as a library.

- [x] Finalize `engine` public API
- [x] Config struct (mode, speed, highway penalties)
- [x] Route → GeoJSON pipeline
- [x] Deterministic tests
- [x] Benchmark routing performance (A*, RAPTOR, engine)
- [x] Improve graph memory layout

Deliverable:

- Stable `pkg/pathcraft/engine`

---

## Phase 0.2 – HTTP Server

Goal: Turn Pathcraft into a service.

- [x] HTTP server mode (`pathcraft server`, alias: `pathcraft serve`)
- [x] `/route` endpoint
- [x] `/health` endpoint
- [x] JSON + GeoJSON output
- [x] Opt-in exact-origin CORS support
- [x] Static map viewer

Deliverable:

- Runnable routing server prototype

---

## Phase 0.3 – Transit Routing

Goal: Multimodal routing.

- [x] GTFS ingestion
- [x] RAPTOR algorithm
- [x] Walk + Transit integration (initial coordinate + nearest-stop version)
- [x] Time-dependent multimodal routing (access walk + scheduled transit + timed legs)

Deliverable:

- Public transit and multimodal routing prototype

---

## Phase 0.4 – Performance & Scale

Goal: Establish measurable scale foundations.

- [x] Directed degree-two graph contraction with exact path expansion
- [x] Source-hashed, versioned, atomic graph caches
- [x] OSM parse → graph → contraction → cache preprocessing pipeline
- [x] Concurrent read-only routing with race-tested HTTP-style queries
- [x] Allocation benchmarks and reproducible `pprof` workflow

Deliverable:

- Tested performance foundations for prototype; full contraction hierarchies and horizontal scaling remain future work

---

## Phase 1.0 – Ecosystem

Goal: Expose tested, language-neutral prototype integration surfaces.

- [x] Go SDK documentation with runnable external-package example
- [x] JS bindings (WASM) for in-browser street routing
- [x] Versioned protobuf/gRPC API for street and multimodal routing
- [x] Compile-time plugin system with built-in adapters

Deliverable:

- Documented Go, JavaScript/WASM, gRPC, and plugin entry points; packaging and production API hardening remain future work

---

## Long-Term Vision

Pathcraft aims to be:

> “The open-source routing engine you deploy when you don’t want vendor lock-in.”
