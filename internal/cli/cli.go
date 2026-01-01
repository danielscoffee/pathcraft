package cli

import (
	"flag"
	"fmt"
	"time"

	"github.com/danielscoffee/pathcraft/internal/http"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func PrintUsage() {
	fmt.Println(`
	PathCraft - Walking and transit routing engine
	Usage:
	pathcraft <command> [options]

	Commands:
	parse    Parse OSM file and show statistics
	route    Find route between two points
	server   Start HTTP server with routing endpoints
	help     Show this help message

	Examples:
	pathcraft parse --file map.osm
	pathcraft route --file map.osm --from 1 --to 100
	pathcraft route --file map.osm --from 1 --to 100 --coords
	pathcraft server --file map.osm --addr :8080
	`)
}

func CmdServer(args []string) error {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	file := fs.String("file", "", "OSM file to parse (.osm or .osm.gz)")
	addr := fs.String("addr", ":8080", "HTTP server address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *file == "" {
		return fmt.Errorf("--file is required")
	}

	fmt.Printf("Loading %s...\n", *file)
	e := engine.New()
	if err := e.LoadOSM(*file); err != nil {
		return fmt.Errorf("loading OSM: %w", err)
	}

	fmt.Printf("Starting HTTP server on %s...\n", *addr)
	// TODO: Update http.RunServer to accept engine.Engine interface
	http.RunServer(e.GetGraph(), *addr)
	return nil
}

// TODO: cache parsed OSM / graph for repeated queries
func CmdParse(args []string) error {
	fs := flag.NewFlagSet("parse", flag.ExitOnError)
	file := fs.String("file", "", "OSM file to parse (.osm or .osm.gz)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *file == "" {
		return fmt.Errorf("--file is required")
	}

	fmt.Printf("Parsing %s...\n", *file)
	start := time.Now()

	e := engine.New()
	if err := e.LoadOSM(*file); err != nil {
		return fmt.Errorf("loading OSM: %w", err)
	}

	loadTime := time.Since(start)
	stats := e.Stats()

	fmt.Println()
	fmt.Println("=== Graph Statistics ===")
	fmt.Printf("  Nodes: %d\n", stats.Nodes)
	fmt.Printf("  Edges: %d\n", stats.Edges)

	fmt.Println()
	fmt.Println("=== Timing ===")
	fmt.Printf("  Load & Build: %v\n", loadTime)

	return nil
}

func CmdRoute(args []string) error {
	fs := flag.NewFlagSet("route", flag.ExitOnError)
	file := fs.String("file", "", "OSM file to parse (.osm or .osm.gz)")
	from := fs.Int64("from", 0, "Source node ID")
	to := fs.Int64("to", 0, "Target node ID")
	speed := fs.Float64("speed", mobility.DefaultWalkingSpeedMPS, "Walking speed in m/s (default: 1.4 = 5 km/h)")
	coords := fs.Bool("coords", false, "Include coordinates in output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *file == "" {
		return fmt.Errorf("--file is required")
	}
	if *from == 0 || *to == 0 {
		return fmt.Errorf("--from and --to are required")
	}

	fmt.Printf("Loading %s...\n", *file)
	e := engine.New()
	if err := e.LoadOSM(*file); err != nil {
		return fmt.Errorf("loading OSM: %w", err)
	}

	fmt.Printf("Finding route from %d to %d...\n", *from, *to)
	start := time.Now()

	profile := mobility.NewWalking(*speed)
	req := engine.RouteRequest{
		From:               *from,
		To:                 *to,
		Profile:            profile,
		IncludeCoordinates: *coords,
	}

	res, err := e.Route(req)
	if err != nil {
		return fmt.Errorf("routing: %w", err)
	}

	routeTime := time.Since(start)

	fmt.Println()
	fmt.Println("=== Route Found ===")
	fmt.Printf("  Nodes:    %d\n", len(res.Nodes))
	fmt.Printf("  Distance: %.0f m\n", res.Distance)
	fmt.Printf("  Walk time: %.1f min (at %.1f m/s)\n", res.Duration.Minutes(), *speed)

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
