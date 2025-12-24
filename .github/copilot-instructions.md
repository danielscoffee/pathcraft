You are assisting in a Go project called "exp-pathcraft".

Project goal:
Pathcraft is a general-purpose routing and pathfinding engine focused on walking and public transit.
It is experimental, educational, and correctness-focused rather than production-scale.

Architecture:
- Modular monolith
- Language: Go
- No microservices
- Clear package boundaries
- Simple, readable code preferred over clever abstractions

Routing model:
- Walking / street routing uses A* or Dijkstra over a simplified OSM-derived graph
- Public transit routing uses the RAPTOR algorithm over GTFS data
- A hybrid orchestrator composes walking → transit → walking
- Algorithms are NOT mixed into a single graph

Data sources:
- OpenStreetMap is used only to build a street graph (nodes + edges)
- GTFS is used for transit schedules (stops, routes, trips, stop_times)
- Geometry (shapes, polylines, coordinates) is used ONLY for visualization, never for routing logic

Constraints:
- Time is a first-class concept in transit routing
- Transfers should be explicit and penalized
- Walking access and egress are bounded by configurable limits

Non-goals (do NOT implement or suggest):
- Realtime GTFS
- Machine learning
- Heavy GIS math
- Distributed systems
- Microservices
- Premature optimization
- UI frameworks

Coding guidelines:
- Prefer explicit types over interfaces unless needed
- Avoid global state
- Keep algorithms testable and deterministic
- Favor clarity over performance unless stated otherwise

When suggesting code:
- Respect existing package boundaries
- Do not invent new layers without justification
- Ask for clarification if a requirement is ambiguous
