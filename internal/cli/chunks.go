package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph/builder"
)

func CmdChunks(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("chunks requires the build subcommand")
	}
	if args[0] != "build" {
		return fmt.Errorf("unknown chunks subcommand %q", args[0])
	}

	fs := flag.NewFlagSet("chunks build", flag.ContinueOnError)
	pbf := fs.String("pbf", "", "OSM PBF input file")
	store := fs.String("store", "", "World graph store directory")
	region := fs.String("region", "", "Stable imported region name")
	zoom := fs.Int("zoom", 12, "XYZ routing zoom")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *pbf == "" {
		return fmt.Errorf("--pbf is required")
	}
	if *store == "" {
		return fmt.Errorf("--store is required")
	}
	if *region == "" {
		return fmt.Errorf("--region is required")
	}
	manifest, err := builder.Build(context.Background(), builder.Options{
		PBFPath: *pbf, StorePath: *store, Region: *region, Zoom: *zoom,
	})
	if err != nil {
		return fmt.Errorf("build chunks: %w", err)
	}
	fmt.Printf("Generation: %s\n", manifest.Generation)
	fmt.Printf("Tiles: %d\n", len(manifest.Tiles))
	for _, imported := range manifest.Regions {
		if imported.Name == *region {
			fmt.Printf("Region: %s (%d tiles)\n", imported.Name, len(imported.Tiles))
			break
		}
	}
	return nil
}
