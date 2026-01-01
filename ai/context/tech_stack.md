# Tech Stack

## Core
- **Language**: Go (Golang)
- **Architecture**: Modular Monolith
- **Build Tool**: Make

## Libraries & Tools
- **Routing**: Custom A* implementation
- **Data Parsing**: 
    - OpenStreetMap (OSM) XML parsing
    - GTFS stop_times parsing (RAPTOR-ready)
- **Geo**: Custom Haversine and geometry utils
- **CLI**: Standard library `flag` (implied, or simple wrapper)
- **Testing**: Standard `testing` package

## Infrastructure (Planned/Optional)
- **Containerization**: Docker
- **API**: HTTP (Standard library or simple router)
