package cli

import (
	"flag"
	"fmt"
	"time"
)

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
