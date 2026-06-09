package cli

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func routeProfileForMode(mode string, speed float64) (mobility.Profile, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "walk", "walking", "bus":
		return mobility.NewWalking(speed), nil
	case "car", "driving", "drive":
		return mobility.NewDriving(speed), nil
	case "bike", "bicycle", "cycling":
		if speed <= 0 {
			speed = 4.5
		}
		return mobility.NewWalking(speed), nil
	default:
		return nil, fmt.Errorf("unknown route mode %q (want walk, car, or bike)", mode)
	}
}

func normalizeClockTime(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == len("15:04") {
		return value + ":00"
	}
	return value
}

func CmdRoute(args []string) error {
	fs := flag.NewFlagSet("route", flag.ExitOnError)
	file := fs.String("file", "", "OSM file to parse (.osm or .osm.gz)")
	from := fs.Int64("from", 0, "Source node ID")
	to := fs.Int64("to", 0, "Target node ID")
	fromLat := fs.Float64("from-lat", 0, "Source latitude")
	fromLon := fs.Float64("from-lon", 0, "Source longitude")
	toLat := fs.Float64("to-lat", 0, "Target latitude")
	toLon := fs.Float64("to-lon", 0, "Target longitude")
	mode := fs.String("mode", "walk", "Routing mode: walk, car, or bike")
	speed := fs.Float64("speed", 0, "Override speed in m/s (defaults depend on mode)")
	coords := fs.Bool("coords", false, "Include coordinates in output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *file == "" {
		return fmt.Errorf("--file is required")
	}

	e, err := loadEngine(*file)
	if err != nil {
		return err
	}

	start := time.Now()

	profile, err := routeProfileForMode(*mode, *speed)
	if err != nil {
		return err
	}
	usingCoords := *fromLat != 0 || *fromLon != 0 || *toLat != 0 || *toLon != 0

	var res *engine.RouteResult
	var coordRes *engine.CoordinateRouteResult
	if usingCoords {
		if *fromLat == 0 || *fromLon == 0 || *toLat == 0 || *toLon == 0 {
			return fmt.Errorf("--from-lat, --from-lon, --to-lat, and --to-lon are required for coordinate routing")
		}
		fmt.Printf("Finding route from (%.6f, %.6f) to (%.6f, %.6f)...\n", *fromLat, *fromLon, *toLat, *toLon)
		coordRes, err = e.RouteByCoordinates(engine.CoordinateRouteRequest{
			FromLat:            *fromLat,
			FromLon:            *fromLon,
			ToLat:              *toLat,
			ToLon:              *toLon,
			Profile:            profile,
			IncludeCoordinates: *coords,
		})
		if err != nil {
			return fmt.Errorf("routing: %w", err)
		}
		res = &coordRes.RouteResult
	} else {
		if *from == 0 || *to == 0 {
			return fmt.Errorf("either --from/--to or full coordinate pairs are required")
		}
		fmt.Printf("Finding route from %d to %d...\n", *from, *to)
		res, err = e.Route(engine.RouteRequest{
			From:               *from,
			To:                 *to,
			Profile:            profile,
			IncludeCoordinates: *coords,
		})
		if err != nil {
			return fmt.Errorf("routing: %w", err)
		}
	}

	routeTime := time.Since(start)

	fmt.Println()
	fmt.Println("=== Route Found ===")
	fmt.Printf("  Nodes:    %d\n", len(res.Nodes))
	fmt.Printf("  Distance: %.0f m\n", res.Distance)
	fmt.Printf("  Mode:     %s\n", *mode)
	fmt.Printf("  Duration: %.1f min (at %.1f m/s)\n", res.Duration.Minutes(), profile.Speed())
	if coordRes != nil {
		fmt.Printf("  Snap from: node %d (%.1f m)\n", coordRes.FromNodeID, coordRes.FromSnapDistanceM)
		fmt.Printf("  Snap to:   node %d (%.1f m)\n", coordRes.ToNodeID, coordRes.ToSnapDistanceM)
	}

	fmt.Println()
	fmt.Println("=== Timing ===")
	fmt.Printf("  Route: %v\n", routeTime)

	fmt.Println()
	fmt.Println("=== Path ===")

	for i, nodeID := range res.Nodes {
		if len(res.Coordinates) > 0 {
			fmt.Printf("  %d. Node %d (%.6f, %.6f)\n", i+1, nodeID, res.Coordinates[i].Lat, res.Coordinates[i].Lon)
		} else {
			fmt.Printf("  %d. Node %d\n", i+1, nodeID)
		}

		if i >= 9 && i < len(res.Nodes)-1 {
			fmt.Printf("  ... (%d more nodes)\n", len(res.Nodes)-i-1)
			if len(res.Coordinates) > 0 {
				lastIdx := len(res.Nodes) - 1
				fmt.Printf("  %d. Node %d (%.6f, %.6f)\n", len(res.Nodes), res.Nodes[lastIdx], res.Coordinates[lastIdx].Lat, res.Coordinates[lastIdx].Lon)
			} else {
				fmt.Printf("  %d. Node %d\n", len(res.Nodes), res.Nodes[len(res.Nodes)-1])
			}
			break
		}
	}

	return nil
}
