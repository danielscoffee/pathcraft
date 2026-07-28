# Tech Stack

## Core

- **Language**: Go (Golang)
- **Architecture**: Modular Monolith
- **Build Tool**: Go toolchain and Make

## Libraries & Tools

- **Routing**: Custom A* implementation
- **Data Parsing**:
  - OpenStreetMap (OSM) XML parsing
  - GTFS stop_times parsing (RAPTOR-ready)
- **Geo**: Custom Haversine and geometry utils
- **CLI**: Standard library `flag`
- **APIs**: Standard library HTTP; grpc-go + Protocol Buffers
- **Browser SDK**: Go `js/wasm`, `syscall/js`, small ES module
- **Frontend**: React, TypeScript, Vite, UnoCSS, Leaflet
- **Testing**: Standard `testing`, gRPC `bufconn`, Node WASM smoke test

## Infrastructure (Planned/Optional)

- **Containerization**: Docker
- **Security**: TLS/auth/rate limits are not built into prototype APIs
