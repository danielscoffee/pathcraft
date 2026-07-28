package cli

import (
	"flag"
	"fmt"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func CmdPreprocess(args []string) error {
	fs := flag.NewFlagSet("preprocess", flag.ExitOnError)
	file := fs.String("file", "", "OSM file to preprocess (.osm or .osm.gz)")
	output := fs.String("output", "", "Output graph cache (default: <file>.cache)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("--file is required")
	}
	if *output == "" {
		*output = *file + ".cache"
	}

	started := time.Now()
	e := engine.New()
	if err := e.LoadOSM(*file); err != nil {
		return err
	}
	if err := e.SaveGraph(*output); err != nil {
		return fmt.Errorf("saving preprocessed graph: %w", err)
	}
	stats := e.Stats()
	fmt.Printf("Preprocessed %d nodes and %d edges into %s (%d contracted nodes, %d directed chains) in %v\n",
		stats.Nodes, stats.Edges, *output, stats.ContractedNodes, stats.ContractionChains, time.Since(started))
	return nil
}
