# PathCraft

PathCraft is an experimental walking and transit routing engine.

## Status
Experimental / Research project

## Features
- Walking routing using A*
- OpenStreetMap (OSM) parsing for walkable paths
- Deterministic, testable core algorithms

### WIP:
- GTFS stop_times parsing (RAPTOR-ready)

## How to use:
First create a .osm file an example is disposable on ./examples/example.osm

```bash
make build
./bin/patchcraft --help # To get help on how to run
```