# gRPC API

PathCraft exposes pre-release protobuf API `pathcraft.v1.RoutingService`. Generated Go client types live beside source contract in [`api/pathcraft/v1`](../../api/pathcraft/v1).

> **Security:** server is plaintext and unauthenticated. Default bind is loopback-only. Do not expose it to untrusted networks without TLS, authentication, authorization, and request controls at trusted proxy or future server layer.

## Start server

Street routes only:

```bash
go build -o bin/pathcraft ./cmd/pathcraft
./bin/pathcraft grpc --file city.osm
```

Street and multimodal journeys:

```bash
./bin/pathcraft grpc --file city.osm --gtfs city-gtfs
```

Default address is `127.0.0.1:9090`. Explicit non-loopback binding is opt-in:

```bash
./bin/pathcraft grpc --file city.osm --addr 0.0.0.0:9090
```

Server registers standard `grpc.health.v1.Health` and reports `SERVING` for `pathcraft.v1.RoutingService` after OSM/optional GTFS loading completes.

## Methods

### `Route`

Coordinate-to-coordinate street route.

```protobuf
rpc Route(RouteRequest) returns (RouteResponse);
```

`RouteRequest` requires `origin` and `destination`. `mode` accepts `TRAVEL_MODE_WALK`, `TRAVEL_MODE_BIKE`, or `TRAVEL_MODE_CAR`; unspecified defaults to walk. `include_coordinates` controls returned polyline points.

Response includes OSM node IDs, optional coordinates, distance, duration in whole seconds, snapped node IDs, and snap distances.

### `Journey`

Timed walk/transit journey.

```protobuf
rpc Journey(JourneyRequest) returns (JourneyResponse);
```

Request requires origin, destination, and GTFS-compatible `HH:MM:SS` departure time. `max_stop_count` uses engine default when zero and cannot exceed 64. Server must start with `--gtfs`.

Response includes total/transit durations, walking distance, chosen stop IDs, and ordered walk/transit/transfer legs.

## Go client

```go
package main

import (
    "context"
    "fmt"

    pathcraftv1 "github.com/danielscoffee/pathcraft/api/pathcraft/v1"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func main() {
    connection, err := grpc.NewClient(
        "127.0.0.1:9090",
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil {
        panic(err)
    }
    defer connection.Close()

    client := pathcraftv1.NewRoutingServiceClient(connection)
    route, err := client.Route(context.Background(), &pathcraftv1.RouteRequest{
        Origin: &pathcraftv1.Coordinate{
            Latitude: -8.05428, Longitude: -34.88130,
        },
        Destination: &pathcraftv1.Coordinate{
            Latitude: -8.05520, Longitude: -34.87970,
        },
        Mode:               pathcraftv1.TravelMode_TRAVEL_MODE_WALK,
        IncludeCoordinates: true,
    })
    if err != nil {
        panic(err)
    }
    fmt.Println(route.DistanceMeters, route.DurationSeconds)
}
```

Use transport credentials instead of `insecure.NewCredentials` once endpoint has TLS.

## grpcurl

Reflection is intentionally disabled. Supply checked-in proto:

```bash
grpcurl -plaintext \
  -import-path . \
  -proto api/pathcraft/v1/pathcraft.proto \
  -d '{
    "origin":{"latitude":-8.05428,"longitude":-34.88130},
    "destination":{"latitude":-8.05520,"longitude":-34.87970},
    "mode":"TRAVEL_MODE_WALK",
    "includeCoordinates":true
  }' \
  127.0.0.1:9090 pathcraft.v1.RoutingService/Route
```

## Status codes

- `InvalidArgument`: missing/malformed coordinates, unsupported mode, invalid departure time, or stop candidate bound.
- `FailedPrecondition`: required OSM or GTFS data is not loaded.
- `NotFound`: engine cannot solve requested route.
- `Canceled` / `DeadlineExceeded`: context ended before or after synchronous engine call.

Current engine search is synchronous and cannot stop midway when context is canceled.

## Regenerate Go types

Generated files currently use `protoc-gen-go v1.36.11` and `protoc-gen-go-grpc v1.6.2`:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2

protoc -I . \
  --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  api/pathcraft/v1/pathcraft.proto
```

Commit `.proto` and generated `.pb.go` files together. API compatibility is not guaranteed before tagged v1 release.
