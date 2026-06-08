package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/http"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func PrintUsage() {
	fmt.Println(`
	PathCraft - Walking and transit routing engine
	Usage:
	pathcraft <command> [options]

	Commands:
	parse    Parse OSM file and show statistics
	route    Find route between two points (walking, node IDs or coordinates)
	transit  Find transit route using RAPTOR algorithm
	journey  Find a walk + transit journey from coordinates
	serve    Start HTTP server with routing endpoints
	server   Alias for serve
	plugins  List registered plugins (algorithms, loaders, exporters)
	pipeline Run loader → algorithm → exporter via plugin registry
	help     Show this help message

	Examples:
	pathcraft parse --file map.osm
	pathcraft route --file map.osm --from 1 --to 100
	pathcraft route --file map.osm --from 1 --to 100 --coords
	pathcraft route --file map.osm --from-lat -8.05428 --from-lon -34.88130 --to-lat -8.05480 --to-lon -34.88030 --coords
	pathcraft transit --gtfs ./examples/gtfs --from RECIFE --to CAMARAGIBE --time 05:00:00
	pathcraft journey --file examples/example.osm --gtfs examples/mini_gtfs --from-lat -8.05428 --from-lon -34.88130 --to-lat -8.05480 --to-lon -34.88030 --time 05:00:00
	pathcraft serve --file map.osm --addr :8080
	`)
}

func CmdServer(args []string) error {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	file := fs.String("file", "", "OSM file to parse (.osm or .osm.gz)")
	gtfsDir := fs.String("gtfs", "", "Directory containing GTFS files for transit and multimodal endpoints")
	addr := fs.String("addr", ":8080", "HTTP server address")
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
	if *gtfsDir != "" {
		if err := e.LoadGTFSDir(*gtfsDir); err != nil {
			return err
		}
	}

	fmt.Printf("Starting HTTP server on %s...\n", *addr)
	http.RunServer(e, *addr)
	return nil
}

func loadEngine(file string) (*engine.Engine, error) {
	e := engine.New()
	cacheFile := file + ".cache"

	if _, err := os.Stat(cacheFile); err == nil {
		fmt.Printf("Loading from cache %s...\n", cacheFile)
		if err := e.LoadGraph(cacheFile); err == nil {
			return e, nil
		}
		fmt.Printf("Cache load failed, falling back to OSM parsing...\n")
	}

	fmt.Printf("Parsing OSM %s...\n", file)
	if err := e.LoadOSM(file); err != nil {
		return nil, err
	}

	fmt.Printf("Saving cache to %s...\n", cacheFile)
	if err := e.SaveGraph(cacheFile); err != nil {
		fmt.Printf("Warning: failed to save cache: %v\n", err)
	}

	return e, nil
}

func CmdParse(args []string) error {
	fs := flag.NewFlagSet("parse", flag.ExitOnError)
	file := fs.String("file", "", "OSM file to parse (.osm or .osm.gz)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *file == "" {
		return fmt.Errorf("--file is required")
	}

	start := time.Now()

	e, err := loadEngine(*file)
	if err != nil {
		return err
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
	fromLat := fs.Float64("from-lat", 0, "Source latitude")
	fromLon := fs.Float64("from-lon", 0, "Source longitude")
	toLat := fs.Float64("to-lat", 0, "Target latitude")
	toLon := fs.Float64("to-lon", 0, "Target longitude")
	speed := fs.Float64("speed", mobility.DefaultWalkingSpeedMPS, "Walking speed in m/s (default: 1.4 = 5 km/h)")
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

	profile := mobility.NewWalking(*speed)
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
	fmt.Printf("  Walk time: %.1f min (at %.1f m/s)\n", res.Duration.Minutes(), *speed)
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

func CmdJourney(args []string) error {
	fs := flag.NewFlagSet("journey", flag.ExitOnError)
	file := fs.String("file", "", "OSM file to parse (.osm or .osm.gz)")
	gtfsDir := fs.String("gtfs", "", "Directory containing GTFS files including stops.txt")
	fromLat := fs.Float64("from-lat", 0, "Source latitude")
	fromLon := fs.Float64("from-lon", 0, "Source longitude")
	toLat := fs.Float64("to-lat", 0, "Target latitude")
	toLon := fs.Float64("to-lon", 0, "Target longitude")
	depTime := fs.String("time", "08:00:00", "Departure time (HH:MM:SS)")
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

	fmt.Printf("Searching multimodal journey from (%.6f, %.6f) to (%.6f, %.6f) at %s...\n", *fromLat, *fromLon, *toLat, *toLon, *depTime)
	start := time.Now()
	res, err := e.MultimodalRoute(engine.MultimodalRouteRequest{
		FromLat:        *fromLat,
		FromLon:        *fromLon,
		ToLat:          *toLat,
		ToLon:          *toLon,
		DepartureTime:  *depTime,
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
			fmt.Printf("  %d. Transfer: %s -> %s\n", i+1, leg.FromName, leg.ToName)
		default:
			fmt.Printf("  %d. Transit %s: %s -> %s\n", i+1, leg.TripID, leg.FromName, leg.ToName)
		}
	}

	return nil
}

func CmdTransit(args []string) error {
	fs := flag.NewFlagSet("transit", flag.ExitOnError)
	gtfsDir := fs.String("gtfs", "", "Directory containing GTFS files (stop_times.txt, trips.txt)")
	from := fs.String("from", "", "Source stop ID")
	to := fs.String("to", "", "Target stop ID")
	depTime := fs.String("time", "08:00:00", "Departure time (HH:MM:SS)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *gtfsDir == "" {
		return fmt.Errorf("--gtfs is required")
	}
	if *from == "" || *to == "" {
		return fmt.Errorf("--from and --to are required")
	}

	stopTimesPath := filepath.Join(*gtfsDir, "stop_times.txt")
	tripsPath := filepath.Join(*gtfsDir, "trips.txt")

	fmt.Printf("Loading GTFS data from %s...\n", *gtfsDir)
	start := time.Now()

	stopTimes, err := gtfs.ParseStopTimesFile(stopTimesPath)
	if err != nil {
		return fmt.Errorf("parsing stop_times.txt: %w", err)
	}

	tripRoutes, err := gtfs.ParseTripsFile(tripsPath)
	if err != nil {
		return fmt.Errorf("parsing trips.txt: %w", err)
	}

	idx := gtfs.BuildIndex(stopTimes, tripRoutes)
	loadTime := time.Since(start)

	fmt.Printf("  Loaded %d stop times, %d trips\n", len(stopTimes), len(tripRoutes))
	fmt.Printf("  Load time: %v\n", loadTime)

	// Parse departure time
	departure, err := pcTime.ParseTime(*depTime)
	if err != nil {
		return fmt.Errorf("invalid departure time: %w", err)
	}

	// Load transfers if available
	transfers := make(map[gtfs.StopID][]raptor.Transfer)
	transfersPath := filepath.Join(*gtfsDir, "transfers.txt")
	if gtfsTransfers, err := gtfs.ParseTransfersFile(transfersPath); err == nil {
		for _, t := range gtfsTransfers {
			transfers[t.FromStopID] = append(transfers[t.FromStopID], raptor.Transfer{
				To:       t.ToStopID,
				Duration: pcTime.Time(t.MinTransferTime),
			})
		}
		fmt.Printf("  Loaded %d transfers\n", len(gtfsTransfers))
	}

	router := raptor.NewRouter(idx, transfers)

	fmt.Printf("\nSearching transit route from %s to %s departing at %s...\n", *from, *to, *depTime)
	routeStart := time.Now()

	result := router.Search(gtfs.StopID(*from), departure)
	routeTime := time.Since(routeStart)

	fmt.Println()
	fmt.Println("=== RAPTOR Search Complete ===")
	fmt.Printf("  Search time: %v\n", routeTime)
	fmt.Printf("  Stops reached: %d\n", len(result.EarliestArrival))

	targetStop := gtfs.StopID(*to)
	arrivalTime, reached := result.EarliestArrival[targetStop]
	if !reached {
		fmt.Printf("\n   Stop %s is not reachable from %s\n", *to, *from)
		fmt.Println("\n  Available stops from source:")
		count := 0
		for stopID, arr := range result.EarliestArrival {
			if count >= 10 {
				fmt.Printf("    ... and %d more\n", len(result.EarliestArrival)-10)
				break
			}
			fmt.Printf("    %s: %s\n", stopID, arr.String())
			count++
		}
		return nil
	}

	fmt.Println()
	fmt.Println("=== Journey Found ===")
	fmt.Printf("  Departure: %s from %s\n", departure.String(), *from)
	fmt.Printf("  Arrival:   %s at %s\n", arrivalTime.String(), *to)
	travelTime := int(arrivalTime - departure)
	fmt.Printf("  Duration:  %d min %d sec\n", travelTime/60, travelTime%60)

	// Reconstruct and display path
	path := result.ReconstructPath(targetStop)
	if len(path) > 0 {
		fmt.Println()
		fmt.Println("=== Journey Steps ===")
		for i, step := range path {
			if step.IsTransfer {
				fmt.Printf("  %d. Transfer: %s → %s\n", i+1, step.FromStop, step.ToStop)
			} else {
				fmt.Printf("  %d. Trip %s: %s → %s\n", i+1, step.TripID, step.FromStop, step.ToStop)
			}
		}
	}

	return nil
}
