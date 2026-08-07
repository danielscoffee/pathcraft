package builder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

const (
	defaultGlobalRunMemoryBytes = int64(512 << 20)
	defaultGlobalPackBytes      = int64(1 << 30)
	defaultGlobalOpenShards     = 64
)

type GlobalOptions struct {
	PBFPath          string
	StorePath        string
	WorkDir          string
	RunMemoryBytes   int64
	PackSegmentBytes int64
	MaxOpenShards    int
	Resume           bool
	DisableResume    bool
	Progress         func(GlobalProgress)
}

type GlobalProgress struct {
	Stage     string
	Completed bool
	Counts    GlobalCounts
	Elapsed   time.Duration
}

func BuildGlobal(ctx context.Context, options GlobalOptions) (worldgraph.Manifest, error) {
	if ctx == nil {
		return worldgraph.Manifest{}, fmt.Errorf("global build context is nil")
	}
	if err := ctx.Err(); err != nil {
		return worldgraph.Manifest{}, err
	}
	if err := normalizeGlobalOptions(&options); err != nil {
		return worldgraph.Manifest{}, err
	}
	started := time.Now()
	source, err := openPBFSource(ctx, options.PBFPath, DefaultMaxWayNodes)
	if err != nil {
		return worldgraph.Manifest{}, err
	}
	defer source.Close()
	store, err := worldgraph.OpenStore(options.StorePath)
	if err != nil {
		return worldgraph.Manifest{}, err
	}
	defer store.Close()

	generation := "global-z12-" + source.Fingerprint()[:20]
	statePath := filepath.Join(options.WorkDir, globalBuildStateFilename)
	builtAt := time.Now().UTC()
	if existing, err := readGlobalBuildState(statePath); err == nil {
		builtAt = existing.BuiltAt
	} else if !errors.Is(err, os.ErrNotExist) {
		return worldgraph.Manifest{}, err
	}
	expected := globalBuildState{
		Version: globalBuildStateVersion, SourcePath: source.Path(), SourceSize: source.Size(),
		SourceSHA256: source.Fingerprint(), RoutingZoom: worldgraph.GlobalRoutingZoom, ShardZoom: worldgraph.PackedShardZoom,
		RunMemoryBytes: options.RunMemoryBytes, PackSegmentBytes: options.PackSegmentBytes,
		MaxOpenShards: options.MaxOpenShards, Generation: generation, BuiltAt: builtAt,
	}
	state, err := loadOrCreateGlobalBuildState(options.WorkDir, expected, options.Resume)
	if err != nil {
		return worldgraph.Manifest{}, err
	}

	persist := func(stage string, completed bool, checkCancellation bool) error {
		state.Counts.WorkBytes, _ = directoryBytes(options.WorkDir)
		state.Counts.StoreBytes, _ = directoryBytes(options.StorePath)
		if err := writeGlobalBuildState(statePath, state); err != nil {
			return err
		}
		if options.Progress != nil {
			options.Progress(GlobalProgress{Stage: stage, Completed: completed, Counts: state.Counts, Elapsed: time.Since(started)})
		}
		if checkCancellation {
			return ctx.Err()
		}
		return nil
	}
	complete := func(stage string) error {
		state.completeStage(stage)
		return persist(stage, true, true)
	}

	current, hasCurrent, err := optionalManifest(store)
	if err != nil {
		return worldgraph.Manifest{}, err
	}
	if hasCurrent && current.Generation == generation && current.Layout == worldgraph.PackedLayout &&
		len(current.Regions) == 1 && current.Regions[0].SourceSHA256 == source.Fingerprint() && current.SourceBytes == source.Size() {
		state.completeStage("publish")
		state.Counts.Shards = int64(len(current.Shards))
		if err := persist("publish", true, false); err != nil {
			return worldgraph.Manifest{}, err
		}
		return current, nil
	}
	state.uncompleteStage("publish")

	if !state.hasStage("validate-source") {
		if err := complete("validate-source"); err != nil {
			return worldgraph.Manifest{}, err
		}
	}

	waysPath := filepath.Join(options.WorkDir, "ways.spool")
	referencesPath := filepath.Join(options.WorkDir, "references.raw")
	if !state.hasStage("spool-ways-and-refs") {
		for _, path := range []string{waysPath, referencesPath} {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return worldgraph.Manifest{}, err
			}
		}
		ways, references, err := spoolGlobalWaysAndReferences(ctx, source, waysPath, referencesPath)
		if err != nil {
			return worldgraph.Manifest{}, err
		}
		state.Counts.Ways, state.Counts.References = ways, references
		if err := complete("spool-ways-and-refs"); err != nil {
			return worldgraph.Manifest{}, err
		}
	}

	sortedReferencesPath := filepath.Join(options.WorkDir, "references.sorted")
	if !state.hasStage("sort-refs") {
		if err := os.Remove(sortedReferencesPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return worldgraph.Manifest{}, err
		}
		runDir := filepath.Join(options.WorkDir, "sort-runs")
		if err := os.RemoveAll(runDir); err != nil {
			return worldgraph.Manifest{}, err
		}
		maxRecords := options.RunMemoryBytes / int64(int64RecordBytes)
		if maxRecords > int64(int(^uint(0)>>1)) {
			maxRecords = int64(int(^uint(0) >> 1))
		}
		if err := sortUniqueInt64File(ctx, referencesPath, sortedReferencesPath, runDir, int(maxRecords)); err != nil {
			return worldgraph.Manifest{}, err
		}
		if err := complete("sort-refs"); err != nil {
			return worldgraph.Manifest{}, err
		}
	}

	nodesPath := filepath.Join(options.WorkDir, "nodes.idx")
	if !state.hasStage("select-nodes") {
		if err := os.Remove(nodesPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return worldgraph.Manifest{}, err
		}
		if err := writeSelectedGlobalNodes(ctx, sortedReferencesPath, nodesPath, func(consume func([]worldgraph.Node) error) error {
			return scanGlobalNodesReader(ctx, source.Reader(), consume)
		}); err != nil {
			return worldgraph.Manifest{}, err
		}
		state.Counts.Nodes, err = globalNodeRecordCount(nodesPath)
		if err != nil {
			return worldgraph.Manifest{}, err
		}
		if err := complete("select-nodes"); err != nil {
			return worldgraph.Manifest{}, err
		}
	}

	contributionRoot := filepath.Join(options.WorkDir, "contributions")
	if !state.hasStage("partition-contributions") {
		if err := os.RemoveAll(contributionRoot); err != nil {
			return worldgraph.Manifest{}, err
		}
		index, err := openGlobalNodeIndex(nodesPath)
		if err != nil {
			return worldgraph.Manifest{}, err
		}
		shards, contributions, partitionErr := partitionGlobalContributions(
			ctx, waysPath, index, contributionRoot, "planet", options.MaxOpenShards, DefaultMaxWayNodes,
		)
		closeErr := index.Close()
		if partitionErr != nil || closeErr != nil {
			return worldgraph.Manifest{}, errors.Join(partitionErr, closeErr)
		}
		if len(shards) == 0 {
			return worldgraph.Manifest{}, fmt.Errorf("global build produced no routable shards")
		}
		state.OccupiedShards = append([]worldgraph.TileID(nil), shards...)
		state.Counts.Shards = int64(len(shards))
		state.Counts.Contributions = contributions
		if err := complete("partition-contributions"); err != nil {
			return worldgraph.Manifest{}, err
		}
	}

	if len(state.OccupiedShards) == 0 {
		return worldgraph.Manifest{}, fmt.Errorf("global build state has no occupied shards")
	}
	manifest := worldgraph.Manifest{
		Generation: generation, Zoom: worldgraph.GlobalRoutingZoom, Bounds: globalShardBounds(state.OccupiedShards),
		Layout: worldgraph.PackedLayout, LayoutVersion: worldgraph.PackedLayoutVersion,
		ShardZoom: worldgraph.PackedShardZoom, Shards: append([]worldgraph.TileID(nil), state.OccupiedShards...),
		SourceBytes: source.Size(), BuiltAt: state.BuiltAt,
		Regions: []worldgraph.RegionManifest{{Name: "planet", SourceSHA256: source.Fingerprint()}},
	}
	stage, err := store.BeginPackedGeneration(manifest)
	if err != nil {
		return worldgraph.Manifest{}, err
	}
	if state.StagePath == "" {
		state.StagePath = stage.Path()
		if err := persist("write-packs", false, true); err != nil {
			return worldgraph.Manifest{}, err
		}
	} else if state.StagePath != stage.Path() {
		return worldgraph.Manifest{}, fmt.Errorf("%w: packed stage path changed", ErrGlobalBuildStateMismatch)
	}
	occupied := make(map[string]worldgraph.TileID, len(state.OccupiedShards))
	for _, shard := range state.OccupiedShards {
		occupied[globalShardStateKey(shard)] = shard
	}
	packed := make(map[string]struct{}, len(state.PackedShards))
	validPacked := make([]string, 0, len(state.PackedShards))
	state.Counts.Chunks, state.Counts.Edges = 0, 0
	recovered := false
	for _, key := range state.PackedShards {
		shard, ok := occupied[key]
		if !ok {
			return worldgraph.Manifest{}, fmt.Errorf("%w: packed shard %q is not occupied", ErrGlobalBuildStateMismatch, key)
		}
		chunks, edges, inspectErr := stage.ValidateShard(ctx, shard)
		if inspectErr == nil {
			state.Counts.Chunks += int64(chunks)
			state.Counts.Edges += edges
			validPacked = append(validPacked, key)
			packed[key] = struct{}{}
			continue
		}
		if err := ctx.Err(); err != nil {
			return worldgraph.Manifest{}, err
		}
		if err := stage.ResetShard(shard); err != nil {
			return worldgraph.Manifest{}, fmt.Errorf("recover packed shard %q after %v: %w", key, inspectErr, err)
		}
		recovered = true
	}
	state.PackedShards = validPacked
	if len(packed) != len(state.OccupiedShards) {
		state.uncompleteStage("write-packs")
	}
	if recovered {
		if err := persist("write-packs", false, true); err != nil {
			return worldgraph.Manifest{}, err
		}
	}
	if !state.hasStage("write-packs") {
		for _, shard := range state.OccupiedShards {
			if err := ctx.Err(); err != nil {
				return worldgraph.Manifest{}, err
			}
			key := globalShardStateKey(shard)
			if _, complete := packed[key]; complete {
				continue
			}
			if err := stage.ResetShard(shard); err != nil {
				return worldgraph.Manifest{}, err
			}
			spoolPath, err := shardSpoolPath(contributionRoot, shard)
			if err != nil {
				return worldgraph.Manifest{}, err
			}
			builtChunks, builtEdges, err := buildPackedShardFromSpool(ctx, spoolPath, shard, stage.Path(), options.PackSegmentBytes)
			if err != nil {
				return worldgraph.Manifest{}, err
			}
			chunks, edges, err := stage.SyncShard(ctx, shard)
			if err != nil {
				return worldgraph.Manifest{}, err
			}
			if chunks != builtChunks || edges != builtEdges {
				return worldgraph.Manifest{}, fmt.Errorf("packed shard %q counts changed after write", key)
			}
			state.Counts.Chunks += int64(chunks)
			state.Counts.Edges += edges
			state.PackedShards = append(state.PackedShards, key)
			sort.Strings(state.PackedShards)
			packed[key] = struct{}{}
			if err := persist("write-packs", false, true); err != nil {
				return worldgraph.Manifest{}, err
			}
		}
		if err := complete("write-packs"); err != nil {
			return worldgraph.Manifest{}, err
		}
	}

	if err := source.Verify(ctx); err != nil {
		return worldgraph.Manifest{}, err
	}
	if err := ctx.Err(); err != nil {
		return worldgraph.Manifest{}, err
	}
	if !state.hasStage("publish") {
		if err := stage.Commit(ctx); err != nil {
			return worldgraph.Manifest{}, err
		}
		state.completeStage("publish")
		if err := persist("publish", true, false); err != nil {
			return worldgraph.Manifest{}, err
		}
	}
	return store.Manifest()
}

func normalizeGlobalOptions(options *GlobalOptions) error {
	if options == nil || options.PBFPath == "" || options.StorePath == "" || options.WorkDir == "" {
		return fmt.Errorf("global build requires PBF, store, and work paths")
	}
	if options.Resume && options.DisableResume {
		return fmt.Errorf("global build resume options conflict")
	}
	if !options.Resume && !options.DisableResume {
		options.Resume = true
	}
	if options.DisableResume {
		options.Resume = false
	}
	if options.RunMemoryBytes == 0 {
		options.RunMemoryBytes = defaultGlobalRunMemoryBytes
	}
	if options.PackSegmentBytes == 0 {
		options.PackSegmentBytes = defaultGlobalPackBytes
	}
	if options.MaxOpenShards == 0 {
		options.MaxOpenShards = defaultGlobalOpenShards
	}
	if options.RunMemoryBytes < int64RecordBytes || options.PackSegmentBytes < 1 || options.MaxOpenShards < 1 {
		return fmt.Errorf("global build resource limits must be positive")
	}
	var err error
	if options.PBFPath, err = filepath.Abs(options.PBFPath); err != nil {
		return err
	}
	if options.StorePath, err = filepath.Abs(options.StorePath); err != nil {
		return err
	}
	if options.WorkDir, err = filepath.Abs(options.WorkDir); err != nil {
		return err
	}
	if options.PBFPath == options.StorePath || options.PBFPath == options.WorkDir || options.StorePath == options.WorkDir {
		return fmt.Errorf("global build paths must be distinct")
	}
	return nil
}

func globalShardBounds(shards []worldgraph.TileID) worldgraph.Bounds {
	bounds := shards[0].Bounds()
	for _, shard := range shards[1:] {
		current := shard.Bounds()
		bounds.West = min(bounds.West, current.West)
		bounds.South = min(bounds.South, current.South)
		bounds.East = max(bounds.East, current.East)
		bounds.North = max(bounds.North, current.North)
	}
	return bounds
}

func globalShardStateKey(shard worldgraph.TileID) string {
	return fmt.Sprintf("%d/%03d/%03d", shard.Z, shard.X, shard.Y)
}

func directoryBytes(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && path == root {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
