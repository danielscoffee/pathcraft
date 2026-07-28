package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	same, err := sameFile(*file, *output)
	if err != nil {
		return fmt.Errorf("validating preprocess paths: %w", err)
	}
	if same {
		return fmt.Errorf("--output must not refer to --file")
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

func sameFile(source, output string) (bool, error) {
	sourcePath, err := filepath.Abs(source)
	if err != nil {
		return false, err
	}
	outputPath, err := filepath.Abs(output)
	if err != nil {
		return false, err
	}
	if filepath.Clean(sourcePath) == filepath.Clean(outputPath) {
		return true, nil
	}

	sourceInfo, err := os.Stat(source)
	if err != nil {
		return false, err
	}
	outputInfo, err := os.Stat(output)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(sourceInfo, outputInfo), nil
}
