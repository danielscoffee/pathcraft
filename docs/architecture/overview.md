# Pathcraft – Architecture Overview

Pathcraft is a modular, multimodal routing engine written in Go.
It is designed to work as:

- a reusable **Go library (SDK)**
- a **CLI application**
- a **small HTTP server for debugging and demos**
- an **embedded engine** inside other systems

The architecture follows **Modular Monolith**.

> An initial evaluation considered a Hexagonal (Ports and Adapters) architecture. However, given
the current scope of the project and its focus on algorithmic correctness, performance, and
rapid iteration, a Modular Monolith provides a better trade-off between clarity, maintainability,
and development velocity.
> The codebase is organized into well-defined modules with explicit boundaries and dependency
direction, ensuring low coupling and high cohesion while avoiding unnecessary architectural
overhead.
> This approach keeps the system easy to reason about today, while still allowing a future
transition to a Hexagonal architecture if the project evolves to require multiple delivery
mechanisms or infrastructure abstractions.

---

## 1. Design Principles

- Core logic must be **pure and deterministic**
- No dependency from core → infra (HTTP, CLI, JSON, files)
- Multiple routing modes (walk, bike, car, transit)
- Multiple execution modes (CLI, HTTP, embedded)
- Easy to extend without rewriting the engine
- Optimized for **correctness first**, performance second

---

## 2. High-Level Architecture

- An example based on this repo:

```mermaid
flowchart TB
    %% Layers
    Interfaces["Interfaces<br/>CLI · HTTP · gRPC · JavaScript/WASM"]

    Plugins["Plugin Registry<br/>Modes · Algorithms · Loaders · Exporters"]

    Engine["Engine Facade<br/>Data · Primitive routing operations"]

    Core["Core Contracts<br/>Graphs · N-dimensional routes"]

    Adapters["Adapters<br/>HTTP · CLI · gRPC · WASM · Visuals"]

    %% Dependency flow (top-down usage, bottom-up dependencies)
    Interfaces --> Adapters
    Adapters --> Plugins
    Plugins --> Engine
    Plugins --> Core
    Engine --> Core
```

---

## 3. Directory Responsibilities

### `/internal`

Private core logic (not exported).

- `graph/`
  - Adjacency list representation
  - Nodes, edges, costs, distances
- `geo/`
  - Haversine distance
  - Heuristics for routing
- `routing/`
  - Algorithms (A*, RAPTOR; future Dijkstra)
- `time/`
  - Time implement time handling but support > 24:00:00 needed by GTFS
- `mobility/`
  - Mobility is the domain of transit entities
- `osm/`
  - OSM parsing → graph adapter
- `gtfs/`
  - GTFS parsing (public transit)
- `geojson/`
  - Conversion of routes to GeoJSON
- `http/`
  - HTTP handlers (adapter layer)
- `grpcapi/`
  - Protobuf/gRPC adapter over the public engine
- `wasmapi/`
  - JSON bridge used by browser WebAssembly bindings

---

### `/pkg/plugins`

Public compile-time registry and built-in implementations. Applications activate plugins through imports; adapters discover registered manifests/capabilities.

### `/pkg/pathcraft/engine`

Primary public Go routing API.

Responsibilities:

- Load data (OSM path or plain XML reader, GTFS directory)
- Validate primitive speed and highway penalties through `Config`
- Expose primitive street/transit operations consumed by mode plugins
- Expose a clean API:
  - `NewWithConfig()`
  - `Route()`
  - `RouteGeoJSON()`
  - `Stats()`

The engine **orchestrates**, it does not compute.

---

### `/api/pathcraft/v1`

Versioned protobuf contract plus generated Go gRPC client/server types.

---

### `/cmd/pathcraft` and `/cmd/pathcraft-wasm`

Native CLI and browser WebAssembly entrypoints.

- Parse transport inputs
- Call engine adapters
- Return CLI, gRPC, or JavaScript results

---

### `/sdk/js`

Small JavaScript loader that starts Go WebAssembly and unwraps bridge results.

---

### `/web`

Visualization and frontend helpers.

---

## 4. Engine as Facade

Example responsibility split:

- Engine:
  - Validate input
  - Choose algorithm
  - Call routing core
- Core:
  - Compute shortest path
- Adapter:
  - Convert to GeoJSON / JSON

This prevents HTTP/CLI concerns from leaking into algorithms.

---

## 5. Routing Modes

- Walking (A*)
- Cycling (A* with pedestrian access rules and configurable speed)
- Driving (A* with access rules and configurable highway penalties)
- Transit (RAPTOR)
- Multimodal (timed Walk + Transit)

Each mode implements `core.Mode`, registers through `pkg/plugins`, declares its coordinate dimensions/options/presentation, and returns generic route segments. The built-in `air` mode demonstrates altitude-preserving 3D output without adding air concepts to core.

---

## 6. Non-Goals

- Real-time traffic (out of scope)
- UI-first design
- Tight coupling to OSM tags

---

## 7. Philosophy

Pathcraft is built as **infrastructure**, not an app.

If Google Maps is a product,
Pathcraft is a **routing engine you own**.
