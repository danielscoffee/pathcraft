# PathCraft

PathCraft is an experimental walking and transit routing engine.

## Status
Experimental / Research project

## Features
- Walking routing using A*
- OpenStreetMap (OSM) parsing for walkable paths
- Deterministic, testable core algorithms

## Documentation
- [Full Documentation](docs/README.md)
- [Architecture Overview](docs/architecture/overview.md)
- [Roadmap](docs/ROADMAP.md)

### WIP:
- GTFS stop_times parsing (RAPTOR-ready)

## How to use:
First create a .osm file an example is available in ./examples/example.osm

```bash
make build
./bin/pathcraft --help # To get help on how to run
```