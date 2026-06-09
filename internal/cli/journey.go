package cli

import (
	"flag"
	"fmt"
	"time"

	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func CmdJourney(args []string) error {
	fs := flag.NewFlagSet("journey", flag.ExitOnError)
	file := fs.String("file", "", "OSM file to parse (.osm or .osm.gz)")
	gtfsDir := fs.String("gtfs", "", "Directory containing GTFS files including stops.txt")
	fromLat := fs.Float64("from-lat", 0, "Source latitude")
	fromLon := fs.Float64("from-lon", 0, "Source longitude")
	toLat := fs.Float64("to-lat", 0, "Target latitude")
	toLon := fs.Float64("to-lon", 0, "Target longitude")
	depTime := fs.String("time", "08:00:00", "Departure time (HH:MM or HH:MM:SS)")
	speed := fs.Float64("speed", mobility.DefaultWalkingSpeedMPS, "Walking speed in m/s")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *file == "" || *gtfsDir == "" {
		return fmt.Errorf("--file and --gtfs are required")
	}
	if *fromLat == 0 || *fromLon == 0 || *toLat == 0 || *toLon == 0 {
		return fmt.Errorf("--from-lat, --from-lon, --to-lat, and --to-lon are required")
	}

	e, err := loadEngine(*file)
	if err != nil {
		return err
	}
	if err := e.LoadGTFSDir(*gtfsDir); err != nil {
		return err
	}

	normalizedDepTime := normalizeClockTime(*depTime)
	fmt.Printf("Searching multimodal journey from (%.6f, %.6f) to (%.6f, %.6f) at %s...\n", *fromLat, *fromLon, *toLat, *toLon, normalizedDepTime)
	start := time.Now()
	res, err := e.MultimodalRoute(engine.MultimodalRouteRequest{
		FromLat:        *fromLat,
		FromLon:        *fromLon,
		ToLat:          *toLat,
		ToLon:          *toLon,
		DepartureTime:  normalizedDepTime,
		WalkingProfile: mobility.NewWalking(*speed),
	})
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("=== Journey Found ===")
	fmt.Printf("  Mode:      %s\n", res.Mode)
	fmt.Printf("  Departure: %s\n", res.DepartureTime)
	fmt.Printf("  Arrival:   %s\n", res.ArrivalTime)
	fmt.Printf("  Duration:  %.1f min\n", res.TotalDuration.Minutes())
	fmt.Printf("  Walking:   %.0f m\n", res.WalkingDistanceM)
	fmt.Printf("  Search:    %v\n", time.Since(start))

	if res.OriginStopID != "" || res.DestinationStopID != "" {
		fmt.Printf("  Transit:   %s -> %s\n", res.OriginStopID, res.DestinationStopID)
	}

	fmt.Println()
	fmt.Println("=== Legs ===")
	for i, leg := range res.Legs {
		switch leg.Mode {
		case "walk":
			fmt.Printf("  %d. Walk: %s -> %s (%.0f m, %.1f min)\n", i+1, leg.FromName, leg.ToName, leg.DistanceM, leg.Duration.Minutes())
		case "transfer":
			fmt.Printf("  %d. Transfer: %s -> %s (%.0f m, %.1f min)\n", i+1, leg.FromName, leg.ToName, leg.DistanceM, leg.Duration.Minutes())
		default:
			line := leg.RouteName
			if line == "" {
				line = leg.RouteID
			}
			if line == "" {
				line = leg.TripID
			}
			if leg.RouteLongName != "" {
				line += " — " + leg.RouteLongName
			}
			fmt.Printf("  %d. Bus %s: %s -> %s\n", i+1, line, leg.FromName, leg.ToName)
		}
	}

	return nil
}
