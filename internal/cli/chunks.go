package cli

import (
	"context"
	"flag"
	"fmt"
	"math"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph/builder"
)

func CmdChunks(args []string) error {
	return CmdChunksContext(context.Background(), args)
}

func CmdChunksContext(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("chunks requires the build subcommand")
	}
	switch args[0] {
	case "build":
		return cmdChunksBuild(ctx, args[1:])
	case "build-global":
		return cmdChunksBuildGlobal(ctx, args[1:])
	default:
		return fmt.Errorf("unknown chunks subcommand %q", args[0])
	}
}

func cmdChunksBuild(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("chunks build", flag.ContinueOnError)
	pbf := fs.String("pbf", "", "OSM PBF input file")
	store := fs.String("store", "", "World graph store directory")
	region := fs.String("region", "", "Stable imported region name")
	zoom := fs.Int("zoom", 12, "XYZ routing zoom")
	if err := fs.Parse(args); err != nil {
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
	manifest, err := builder.Build(ctx, builder.Options{
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

func cmdChunksBuildGlobal(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("chunks build-global", flag.ContinueOnError)
	pbf := fs.String("pbf", "", "Full OSM PBF input file")
	store := fs.String("store", "", "Packed world graph store directory")
	workDir := fs.String("work-dir", "", "Restartable build work directory")
	runMemoryMB := fs.Int64("run-memory-mb", 512, "External-sort memory in MiB")
	packMB := fs.Int64("pack-mb", 1024, "Maximum pack segment size in MiB")
	openShards := fs.Int("open-shards", 64, "Maximum open shard spool files")
	resume := fs.Bool("resume", true, "Resume matching completed work")
	zoom := fs.Int("zoom", 12, "Fixed global routing zoom")
	if err := fs.Parse(args); err != nil {
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
	if *workDir == "" {
		return fmt.Errorf("--work-dir is required")
	}
	if *zoom != 12 {
		return fmt.Errorf("global routing zoom is fixed at 12")
	}
	if *runMemoryMB < 1 || *packMB < 1 || *openShards < 1 {
		return fmt.Errorf("global resource options must be positive")
	}
	if *runMemoryMB > math.MaxInt64>>20 || *packMB > math.MaxInt64>>20 {
		return fmt.Errorf("global resource options overflow bytes")
	}

	started := time.Now()
	var counts builder.GlobalCounts
	manifest, err := builder.BuildGlobal(ctx, builder.GlobalOptions{
		PBFPath: *pbf, StorePath: *store, WorkDir: *workDir,
		RunMemoryBytes: *runMemoryMB << 20, PackSegmentBytes: *packMB << 20,
		MaxOpenShards: *openShards, Resume: *resume, DisableResume: !*resume,
		Progress: func(progress builder.GlobalProgress) {
			counts = progress.Counts
			status := "running"
			if progress.Completed {
				status = "complete"
			}
			fmt.Printf("Stage: %s (%s, %s)\n", progress.Stage, status, progress.Elapsed.Round(time.Millisecond))
		},
	})
	if err != nil {
		return fmt.Errorf("build global chunks: %w", err)
	}
	fmt.Printf("Generation: %s\n", manifest.Generation)
	fmt.Printf("Shards: %d\n", counts.Shards)
	fmt.Printf("Chunks: %d\n", counts.Chunks)
	fmt.Printf("Ways: %d\n", counts.Ways)
	fmt.Printf("References: %d\n", counts.References)
	fmt.Printf("Nodes: %d\n", counts.Nodes)
	fmt.Printf("Segments: %d\n", counts.Segments)
	fmt.Printf("Fragments: %d\n", counts.Fragments)
	fmt.Printf("Edges: %d\n", counts.Edges)
	fmt.Printf("Contributions: %d\n", counts.Contributions)
	fmt.Printf("Work bytes: %d\n", counts.WorkBytes)
	fmt.Printf("Store bytes: %d\n", counts.StoreBytes)
	fmt.Printf("Elapsed: %s\n", time.Since(started).Round(time.Millisecond))
	return nil
}
