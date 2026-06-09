package cli

import (
	"flag"
	"fmt"
	"path/filepath"
	"time"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
)

func transitLineLabel(tripID gtfs.TripID, tripRoutes gtfs.TripToRoute, routes map[gtfs.RouteID]gtfs.Route) string {
	if routeID, ok := tripRoutes[tripID]; ok {
		if route, ok := routes[routeID]; ok {
			label := route.ShortName
			if label == "" {
				label = string(routeID)
			}
			if route.LongName != "" {
				label += " — " + route.LongName
			}
			return label
		}
		return string(routeID)
	}
	return string(tripID)
}

func inferTransitTransfers(stops map[gtfs.StopID]gtfs.Stop, maxDistanceM float64, minSeconds int) map[gtfs.StopID][]raptor.Transfer {
	out := make(map[gtfs.StopID][]raptor.Transfer)
	list := make([]gtfs.Stop, 0, len(stops))
	for _, stop := range stops {
		list = append(list, stop)
	}
	for i, from := range list {
		for j, to := range list {
			if i == j {
				continue
			}
			d := geo.HaversineDistance(from.Lat, from.Lon, to.Lat, to.Lon)
			if d > maxDistanceM {
				continue
			}
			seconds := int(d / mobility.DefaultWalkingSpeedMPS)
			if seconds < minSeconds {
				seconds = minSeconds
			}
			out[from.ID] = append(out[from.ID], raptor.Transfer{To: to.ID, Duration: pcTime.Time(seconds)})
		}
	}
	return out
}

func CmdTransit(args []string) error {
	fs := flag.NewFlagSet("transit", flag.ExitOnError)
	gtfsDir := fs.String("gtfs", "", "Directory containing GTFS files (stop_times.txt, trips.txt)")
	from := fs.String("from", "", "Source stop ID")
	to := fs.String("to", "", "Target stop ID")
	depTime := fs.String("time", "08:00:00", "Departure time (HH:MM or HH:MM:SS)")
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
	routes, _ := gtfs.ParseRoutesFile(filepath.Join(*gtfsDir, "routes.txt"))

	idx := gtfs.BuildIndex(stopTimes, tripRoutes)
	loadTime := time.Since(start)

	fmt.Printf("  Loaded %d stop times, %d trips\n", len(stopTimes), len(tripRoutes))
	fmt.Printf("  Load time: %v\n", loadTime)

	// Parse departure time
	normalizedDepTime := normalizeClockTime(*depTime)
	departure, err := pcTime.ParseTime(normalizedDepTime)
	if err != nil {
		return fmt.Errorf("invalid departure time: %w", err)
	}

	// Load transfers if available; otherwise infer nearby stop walking transfers.
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
	} else if stops, err := gtfs.ParseStopsFile(filepath.Join(*gtfsDir, "stops.txt")); err == nil {
		transfers = inferTransitTransfers(stops, 80, 120)
		fmt.Printf("  Inferred nearby stop transfers for %d stops\n", len(transfers))
	}

	router := raptor.NewRouter(idx, transfers)

	fmt.Printf("\nSearching transit route from %s to %s departing at %s...\n", *from, *to, normalizedDepTime)
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
				line := transitLineLabel(step.TripID, tripRoutes, routes)
				fmt.Printf("  %d. Bus %s: %s → %s\n", i+1, line, step.FromStop, step.ToStop)
			}
		}
	}

	return nil
}
