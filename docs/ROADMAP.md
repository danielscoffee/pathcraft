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
- [ ] Config struct (speed, mode, penalties)
- [ ] Route → GeoJSON pipeline
- [ ] Deterministic tests
- [ ] Benchmark routing performance
- [ ] Improve graph memory layout

Deliverable:
- Stable `pkg/pathcraft/engine`

---

## Phase 0.2 – HTTP Server 

Goal: Turn Pathcraft into a service.

- [ ] HTTP server mode (`pathcraft serve`)
- [ ] `/route` endpoint
- [ ] `/health` endpoint
- [ ] JSON + GeoJSON output
- [ ] CORS support
- [ ] Static map viewer

Deliverable:
- Docker-ready routing server

---

## Phase 0.3 – Transit Routing

Goal: Multimodal routing.

- [ ] GTFS ingestion
- [ ] RAPTOR algorithm
- [ ] Walk + Transit integration
- [ ] Time-dependent routing
- [ ] Schedule-based heuristics

Deliverable:
- Public transit routing engine

---

## Phase 0.4 – Performance & Scale

Goal: Serious engine.

- [ ] Graph contraction
- [ ] Caching strategies
- [ ] Preprocessing pipelines
- [ ] Parallel routing
- [ ] Memory profiling

---

## Phase 1.0 – Ecosystem

- [ ] Go SDK documentation
- [ ] JS bindings (WASM)
- [ ] gRPC API
- [ ] Plugin system

---

## Long-Term Vision

Pathcraft aims to be:

> “The open-source routing engine you deploy when you don’t want vendor lock-in.”
