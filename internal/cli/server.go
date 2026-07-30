package cli

import (
	"flag"
	"fmt"
	"math"
	"strings"

	"github.com/danielscoffee/pathcraft/internal/http"
	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

const defaultHTTPAddress = "127.0.0.1:8080"

var runHTTPServer = http.RunServerWithHost

func CmdServer(args []string) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	file := fs.String("file", "", "OSM file to parse (.osm or .osm.gz)")
	chunks := fs.String("chunks", "", "World graph chunk store")
	gtfsDir := fs.String("gtfs", "", "Directory containing GTFS files for transit and multimodal endpoints")
	cacheMB := fs.Int64("chunk-cache-mb", 512, "Decoded world chunk cache in MiB")
	maxTiles := fs.Int("route-max-tiles", 256, "Maximum world graph tiles per route")
	maxExpansions := fs.Int("route-max-expansions", 3, "Maximum world graph corridor expansions")
	addr := fs.String("addr", defaultHTTPAddress, "HTTP server address")
	corsOrigin := fs.String("cors-origin", "", "Comma-separated exact origins allowed for browser API requests")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if (*file == "") == (*chunks == "") {
		return fmt.Errorf("exactly one of --file or --chunks is required")
	}
	if *chunks != "" && *gtfsDir != "" {
		return fmt.Errorf("--gtfs is not supported with --chunks")
	}
	if *cacheMB <= 0 || *cacheMB > math.MaxInt64/(1<<20) {
		return fmt.Errorf("--chunk-cache-mb must be a positive supported size")
	}
	if *maxTiles <= 0 {
		return fmt.Errorf("--route-max-tiles must be positive")
	}
	if *maxExpansions <= 0 {
		return fmt.Errorf("--route-max-expansions must be positive")
	}

	var host any
	if *file != "" {
		e, err := loadEngine(*file)
		if err != nil {
			return err
		}
		if *gtfsDir != "" {
			if err := e.LoadGTFSDir(*gtfsDir); err != nil {
				return err
			}
		}
		host = e
	} else {
		router, err := worldgraph.OpenRouter(*chunks, worldgraph.RouterOptions{
			CacheBytes: *cacheMB << 20, MaxTiles: *maxTiles, MaxExpansions: *maxExpansions,
		})
		if err != nil {
			return err
		}
		defer router.Close()
		host = router
	}

	fmt.Printf("Starting HTTP server on %s...\n", *addr)
	runHTTPServer(host, *addr, parseCORSOrigins(*corsOrigin)...)
	return nil
}

func parseCORSOrigins(value string) []string {
	var origins []string
	for origin := range strings.SplitSeq(value, ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}
