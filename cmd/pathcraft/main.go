package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	m "github.com/qoppatech/exp-pathcraft/internal/domain/mobility"
	t "github.com/qoppatech/exp-pathcraft/internal/domain/time"
	"github.com/qoppatech/exp-pathcraft/internal/geo"
	"github.com/qoppatech/exp-pathcraft/internal/graph"
	"github.com/qoppatech/exp-pathcraft/internal/http"
	"github.com/qoppatech/exp-pathcraft/internal/osm"
	astar "github.com/qoppatech/exp-pathcraft/internal/routing/astar"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		printUsage()
		return nil
	}

	switch os.Args[1] {
	case "parse":
		return cmdParse(os.Args[2:])
	case "route":
		return cmdRoute(os.Args[2:])
	case "server":
		return cmdServer(os.Args[2:])
	case "help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
}

func printUsage() {
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
	pathcraft server --file map.osm --addr :8080
	`)
}

func cmdServer(args []string) error {
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
	data, err := osm.ParseFile(*file)
	if err != nil {
		return fmt.Errorf("parsing OSM: %w", err)
	}

	g := osm.BuildGraph(data, nil)

	fmt.Printf("Starting HTTP server on %s...\n", *addr)
	http.RunServer(g, *addr)
	return nil
}

// TODO: cache parsed OSM / graph for repeated queries
func cmdParse(args []string) error {
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

	data, err := osm.ParseFile(*file)
	if err != nil {
		return fmt.Errorf("parsing OSM: %w", err)
	}

	parseTime := time.Since(start)

	start = time.Now()
	g := osm.BuildGraph(data, nil)
	buildTime := time.Since(start)

	edgeCount := 0
	for _, edges := range g.Edges {
		edgeCount += len(edges)
	}

	fmt.Println()
	fmt.Println("=== OSM Data ===")
	fmt.Printf("  Nodes: %d\n", len(data.Nodes))
	fmt.Printf("  Ways:  %d\n", len(data.Ways))

	fmt.Println()
	fmt.Println("=== Street Graph ===")
	fmt.Printf("  Nodes: %d\n", len(g.Nodes))
	fmt.Printf("  Edges: %d\n", edgeCount)

	fmt.Println()
	fmt.Println("=== Timing ===")
	fmt.Printf("  Parse: %v\n", parseTime)
	fmt.Printf("  Build: %v\n", buildTime)

	return nil
}

func cmdRoute(args []string) error {
	fs := flag.NewFlagSet("route", flag.ExitOnError)
	file := fs.String("file", "", "OSM file to parse (.osm or .osm.gz)")
	from := fs.Int64("from", 0, "Source node ID")
	to := fs.Int64("to", 0, "Target node ID")
	speed := fs.Float64("speed", m.DefaultWalkingSpeedMPS, "Walking speed in m/s (default: 1.4 = 5 km/h)")
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
	data, err := osm.ParseFile(*file)
	if err != nil {
		return fmt.Errorf("parsing OSM: %w", err)
	}

	g := osm.BuildGraph(data, nil)

	sourceID := graph.NodeID(*from)
	targetID := graph.NodeID(*to)

	if !g.HasNode(sourceID) {
		return fmt.Errorf("source node %d not found in graph", *from)
	}
	if !g.HasNode(targetID) {
		return fmt.Errorf("target node %d not found in graph", *to)
	}

	fmt.Printf("Finding route from %d to %d...\n", *from, *to)
	start := time.Now()

	// FIXME: Put heuristic outside?
	heuristic := geo.HaversineHeuristic(*speed)
	path, err := astar.AStar(g, sourceID, targetID, heuristic)
	if err != nil {
		return fmt.Errorf("routing: %w", err)
	}

	routeTime := time.Since(start)

	totalDistance := path.TotalCost // in meters

	fmt.Println()
	fmt.Println("=== Route Found ===")
	fmt.Printf("  Nodes:    %d\n", path.NodesCount)
	fmt.Printf("  Distance: %.0f m\n", totalDistance)
	fmt.Printf("  Walk time: %.1f min (at %.1f m/s)\n", totalDistance/(*speed)/t.SecondsPerMinute, *speed)

	fmt.Println()
	fmt.Println("=== Timing ===")
	fmt.Printf("  Route: %v\n", routeTime)

	fmt.Println()
	fmt.Println("=== Path ===")
	for i, nodeID := range path.Nodes {
		node := g.Nodes[nodeID]
		fmt.Printf(
			"  %d. Node %d (%.6f, %.6f)\n",
			i+1,
			nodeID,
			node.Lat,
			node.Lon,
		)
		if i >= 9 && i < len(path.Nodes)-1 {
			fmt.Printf("  ... (%d more nodes)\n", len(path.Nodes)-i-1)
			node := g.Nodes[path.Nodes[len(path.Nodes)-1]]
			fmt.Printf(
				"  %d. Node %d (%.6f, %.6f)\n",
				len(path.Nodes),
				path.Nodes[len(path.Nodes)-1],
				node.Lat,
				node.Lon,
			)
			break
		}
	}

	return nil
}
