package cli

import (
	"context"
	"flag"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

func normalizeClockTime(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == len("15:04") {
		return value + ":00"
	}
	return value
}

func CmdRoute(args []string) error {
	fs := flag.NewFlagSet("route", flag.ExitOnError)
	file := fs.String("file", "", "OSM file when the selected plugin needs a street graph")
	from := fs.Int64("from", 0, "Source node ID")
	to := fs.Int64("to", 0, "Target node ID")
	fromLat := fs.Float64("from-lat", 0, "Source latitude")
	fromLon := fs.Float64("from-lon", 0, "Source longitude")
	toLat := fs.Float64("to-lat", 0, "Target latitude")
	toLon := fs.Float64("to-lon", 0, "Target longitude")
	fromPosition := fs.String("from-position", "", "Plugin-defined comma-separated source position")
	toPosition := fs.String("to-position", "", "Plugin-defined comma-separated target position")
	modeName := fs.String("mode", "walk", "Registered routing mode")
	speed := fs.Float64("speed", 0, "Shortcut for --opt speed_mps=<value>")
	coords := fs.Bool("coords", false, "Print route positions")
	var rawOptions multiFlag
	fs.Var(&rawOptions, "opt", "Mode option key=value (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	modeID := strings.ToLower(strings.TrimSpace(*modeName))
	mode, ok := plugins.Default.Mode(modeID)
	if !ok {
		return fmt.Errorf("routing mode %q not registered", modeID)
	}

	router := engine.New()
	var err error
	if *file != "" {
		router, err = loadEngine(*file)
		if err != nil {
			return err
		}
	}
	fromPoint, toPoint, err := cliRoutePositions(router, *fromPosition, *toPosition, *from, *to, *fromLon, *fromLat, *toLon, *toLat)
	if err != nil {
		return err
	}
	options, err := parseModeOptions(rawOptions)
	if err != nil {
		return err
	}
	if *speed > 0 {
		options["speed_mps"] = strconv.FormatFloat(*speed, 'g', -1, 64)
	}

	started := time.Now()
	result, err := mode.Route(context.Background(), router, core.ModeRequest{
		From:    fromPoint,
		To:      toPoint,
		Options: options,
	})
	if err != nil {
		return fmt.Errorf("routing: %w", err)
	}

	nodes, _ := result.Meta["nodes"].([]int64)
	positions := modeResultPositions(result)
	fmt.Println()
	fmt.Println("=== Route Found ===")
	fmt.Printf("  Mode:      %s\n", result.Mode)
	fmt.Printf("  Segments:  %d\n", len(result.Segments))
	fmt.Printf("  Nodes:     %d\n", len(nodes))
	fmt.Printf("  Positions: %d\n", len(positions))
	fmt.Printf("  Distance:  %.0f m\n", result.DistanceMeters)
	fmt.Printf("  Duration:  %.1f min\n", float64(result.DurationSeconds)/60)
	fmt.Printf("  Route:     %v\n", time.Since(started))

	if *coords {
		fmt.Println()
		fmt.Println("=== Path ===")
		for i, position := range positions {
			fmt.Printf("  %d. %v\n", i+1, []float64(position))
			if i >= 9 && i < len(positions)-1 {
				fmt.Printf("  ... (%d more positions)\n", len(positions)-i-1)
				break
			}
		}
	}
	return nil
}

func cliRoutePositions(router *engine.Engine, fromPosition, toPosition string, fromID, toID int64, fromLon, fromLat, toLon, toLat float64) (core.Position, core.Position, error) {
	if fromPosition != "" || toPosition != "" {
		from, err := parseCLIPosition("from", fromPosition)
		if err != nil {
			return nil, nil, err
		}
		to, err := parseCLIPosition("to", toPosition)
		return from, to, err
	}
	if fromLat != 0 || fromLon != 0 || toLat != 0 || toLon != 0 {
		if fromLat == 0 || fromLon == 0 || toLat == 0 || toLon == 0 {
			return nil, nil, fmt.Errorf("--from-lat, --from-lon, --to-lat, and --to-lon are required together")
		}
		return core.Position{fromLon, fromLat}, core.Position{toLon, toLat}, nil
	}
	if fromID == 0 || toID == 0 {
		return nil, nil, fmt.Errorf("provide --from-position/--to-position, coordinates, or node IDs")
	}
	g := router.GetGraph()
	if g == nil {
		return nil, nil, fmt.Errorf("selected node IDs require a loaded graph")
	}
	fromNode, fromOK := g.Nodes[graph.NodeID(fromID)]
	toNode, toOK := g.Nodes[graph.NodeID(toID)]
	if !fromOK || !toOK {
		return nil, nil, fmt.Errorf("route node not found")
	}
	return core.Position{fromNode.Lon, fromNode.Lat}, core.Position{toNode.Lon, toNode.Lat}, nil
}

func parseCLIPosition(name, value string) (core.Position, error) {
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("--%s-position is required", name)
	}
	parts := strings.Split(value, ",")
	position := make(core.Position, len(parts))
	for i, part := range parts {
		coordinate, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || math.IsNaN(coordinate) || math.IsInf(coordinate, 0) {
			return nil, fmt.Errorf("invalid --%s-position", name)
		}
		position[i] = coordinate
	}
	return position, nil
}

func parseModeOptions(values []string) (map[string]string, error) {
	options := make(map[string]string, len(values))
	for _, value := range values {
		key, option, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("--opt expects key=value, got %q", value)
		}
		options[strings.TrimSpace(key)] = option
	}
	return options, nil
}

func modeResultPositions(result core.ModeResult) []core.Position {
	var positions []core.Position
	for _, segment := range result.Segments {
		positions = append(positions, segment.Positions...)
	}
	return positions
}
