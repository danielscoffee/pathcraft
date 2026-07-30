package cli

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"time"

	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
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

	router, err := loadEngine(*file)
	if err != nil {
		return err
	}
	if err := router.LoadGTFSDir(*gtfsDir); err != nil {
		return err
	}
	mode, ok := plugins.Default.Mode("gtfs")
	if !ok {
		return fmt.Errorf("routing mode %q not registered", "gtfs")
	}

	normalizedDepTime := normalizeClockTime(*depTime)
	fmt.Printf("Searching journey from (%.6f, %.6f) to (%.6f, %.6f) at %s...\n", *fromLat, *fromLon, *toLat, *toLon, normalizedDepTime)
	started := time.Now()
	result, err := mode.Route(context.Background(), router, core.ModeRequest{
		From: core.Position{*fromLon, *fromLat},
		To:   core.Position{*toLon, *toLat},
		Options: map[string]string{
			"departure_time":    normalizedDepTime,
			"walking_speed_mps": strconv.FormatFloat(*speed, 'g', -1, 64),
		},
	})
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("=== Journey Found ===")
	fmt.Printf("  Mode:      %s\n", result.Mode)
	fmt.Printf("  Departure: %v\n", result.Meta["departure_time"])
	fmt.Printf("  Arrival:   %v\n", result.Meta["arrival_time"])
	fmt.Printf("  Duration:  %.1f min\n", float64(result.DurationSeconds)/60)
	fmt.Printf("  Walking:   %.0f m\n", result.DistanceMeters)
	fmt.Printf("  Search:    %v\n", time.Since(started))

	fmt.Println()
	fmt.Println("=== Legs ===")
	for i, segment := range result.Segments {
		fmt.Printf("  %d. %s: %v -> %v (%.0f m, %.1f min)\n",
			i+1,
			segment.Label,
			segment.Meta["from"],
			segment.Meta["to"],
			segment.DistanceMeters,
			float64(segment.DurationSeconds)/60,
		)
	}
	return nil
}
