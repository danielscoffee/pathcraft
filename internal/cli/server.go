package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/danielscoffee/pathcraft/internal/http"
)

func CmdServer(args []string) error {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	file := fs.String("file", "", "OSM file to parse (.osm or .osm.gz)")
	gtfsDir := fs.String("gtfs", "", "Directory containing GTFS files for transit and multimodal endpoints")
	addr := fs.String("addr", ":8080", "HTTP server address")
	corsOrigin := fs.String("cors-origin", "", "Comma-separated exact origins allowed for browser API requests")
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
	http.RunServer(e, *addr, parseCORSOrigins(*corsOrigin)...)
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
