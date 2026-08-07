package builder

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

const (
	defaultGlobalRunMemoryBytes    = int64(512 << 20)
	defaultGlobalPackBytes         = int64(1 << 30)
	defaultGlobalOpenShards        = 64
	resolvedWayNodeSortMemoryBytes = 64 // Includes the in-memory TileID fields omitted from the 32-byte record.
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
	if err := validateLegacyGlobalBuildStageFrontier(state); err != nil {
		return worldgraph.Manifest{}, err
	}
	if err := removeGlobalBuildTemporaryMatches(options.WorkDir,
		".build-state-*.tmp", ".ways-*.tmp", ".refs-*.tmp", ".sorted-*.tmp", ".nodes-*.tmp",
		".way-node-requests-*.tmp", ".resolved-way-nodes-*.tmp", ".fixed-sort-output-*.tmp",
	); err != nil {
		return worldgraph.Manifest{}, err
	}

	persist := func(stage string, completed bool, checkCancellation bool) error {
		workBytes, measureErr := directoryBytes(options.WorkDir)
		if measureErr != nil {
			return fmt.Errorf("measure global work directory: %w", measureErr)
		}
		storeBytes, measureErr := directoryBytes(options.StorePath)
		if measureErr != nil {
			return fmt.Errorf("measure global store directory: %w", measureErr)
		}
		state.Counts.WorkBytes = workBytes
		state.Counts.StoreBytes = storeBytes
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
		state.Version = globalBuildStateVersion
		state.completeStage("publish")
		state.Counts.Shards = int64(len(current.Shards))
		if err := persist("publish", true, false); err != nil {
			return worldgraph.Manifest{}, err
		}
		return current, nil
	}
	state.uncompleteStage("publish")
	if state.hasStage("partition-contributions") && !state.hasStage("partition-fragments-v2") {
		return worldgraph.Manifest{}, fmt.Errorf("%w: legacy contribution partition cannot resume with partition v2; use a fresh work directory", ErrGlobalBuildStateMismatch)
	}
	if state.Version == 1 && len(state.Completed) > 4 {
		return worldgraph.Manifest{}, fmt.Errorf("%w: legacy contribution partition cannot resume with partition v2; use a fresh work directory", ErrGlobalBuildStateMismatch)
	}

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
	if state.Version != 1 && state.hasStage("sort-refs") {
		if err := removeGlobalBuildArtifacts(referencesPath); err != nil {
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

	if state.Version == 1 {
		if err := validateLegacyGlobalBuildArtifacts(waysPath, referencesPath, sortedReferencesPath, nodesPath, state.Counts); err != nil {
			return worldgraph.Manifest{}, err
		}
		// A legacy partition never checkpointed partial output. Remove it only
		// after proving that every checkpointed input needed by v2 is intact.
		if err := os.RemoveAll(filepath.Join(options.WorkDir, "contributions")); err != nil {
			return worldgraph.Manifest{}, err
		}
	}
	if err := removeGlobalBuildArtifacts(referencesPath, sortedReferencesPath); err != nil {
		return worldgraph.Manifest{}, err
	}

	requestsRawPath := filepath.Join(options.WorkDir, "way-node-requests.raw")
	if !state.hasStage("spool-way-node-requests-v2") {
		if err := os.Remove(requestsRawPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return worldgraph.Manifest{}, err
		}
		counts, err := spoolWayNodeRequestsWithCounts(ctx, waysPath, requestsRawPath, DefaultMaxWayNodes)
		if err != nil {
			return worldgraph.Manifest{}, err
		}
		if counts.Ways != state.Counts.Ways || counts.References != state.Counts.References {
			_ = os.Remove(requestsRawPath)
			return worldgraph.Manifest{}, fmt.Errorf("%w: way-node request spool has %d ways and %d references, want %d and %d", ErrGlobalBuildStateMismatch, counts.Ways, counts.References, state.Counts.Ways, state.Counts.References)
		}
		state.Version = globalBuildStateVersion
		if err := complete("spool-way-node-requests-v2"); err != nil {
			return worldgraph.Manifest{}, err
		}
	}

	requestsSortedPath := filepath.Join(options.WorkDir, "way-node-requests.sorted")
	requestRunsPath := filepath.Join(options.WorkDir, "way-node-request-runs")
	if !state.hasStage("sort-way-node-requests-v2") {
		if err := os.Remove(requestsSortedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return worldgraph.Manifest{}, err
		}
		if err := os.RemoveAll(requestRunsPath); err != nil {
			return worldgraph.Manifest{}, err
		}
		if err := sortWayNodeRequests(ctx, requestsRawPath, requestsSortedPath, requestRunsPath,
			globalSortRecordLimit(options.RunMemoryBytes, wayNodeRequestRecordBytes)); err != nil {
			return worldgraph.Manifest{}, err
		}
		if err := complete("sort-way-node-requests-v2"); err != nil {
			return worldgraph.Manifest{}, err
		}
	}
	if state.hasStage("sort-way-node-requests-v2") {
		if err := removeGlobalBuildArtifacts(requestsRawPath, requestRunsPath); err != nil {
			return worldgraph.Manifest{}, err
		}
	}

	resolvedRawPath := filepath.Join(options.WorkDir, "resolved-way-nodes.raw")
	if !state.hasStage("join-way-node-requests-v2") {
		if err := os.Remove(resolvedRawPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return worldgraph.Manifest{}, err
		}
		count, err := joinWayNodeRequests(ctx, requestsSortedPath, nodesPath, resolvedRawPath)
		if err != nil {
			return worldgraph.Manifest{}, err
		}
		if count != state.Counts.References {
			return worldgraph.Manifest{}, fmt.Errorf("%w: resolved way-node count %d, want %d", ErrGlobalBuildStateMismatch, count, state.Counts.References)
		}
		if err := complete("join-way-node-requests-v2"); err != nil {
			return worldgraph.Manifest{}, err
		}
	}
	if state.hasStage("join-way-node-requests-v2") {
		if err := removeGlobalBuildArtifacts(requestsSortedPath, nodesPath); err != nil {
			return worldgraph.Manifest{}, err
		}
	}

	resolvedSortedPath := filepath.Join(options.WorkDir, "resolved-way-nodes.sorted")
	resolvedRunsPath := filepath.Join(options.WorkDir, "resolved-way-node-runs")
	if !state.hasStage("sort-resolved-way-nodes-v2") {
		if err := os.Remove(resolvedSortedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return worldgraph.Manifest{}, err
		}
		if err := os.RemoveAll(resolvedRunsPath); err != nil {
			return worldgraph.Manifest{}, err
		}
		if err := sortResolvedWayNodes(ctx, resolvedRawPath, resolvedSortedPath, resolvedRunsPath,
			globalSortRecordLimit(options.RunMemoryBytes, resolvedWayNodeSortMemoryBytes)); err != nil {
			return worldgraph.Manifest{}, err
		}
		if err := complete("sort-resolved-way-nodes-v2"); err != nil {
			return worldgraph.Manifest{}, err
		}
	}
	if state.hasStage("sort-resolved-way-nodes-v2") {
		if err := removeGlobalBuildArtifacts(resolvedRawPath, resolvedRunsPath); err != nil {
			return worldgraph.Manifest{}, err
		}
	}

	fragmentRoot := filepath.Join(options.WorkDir, "fragments-v2")
	if !state.hasStage("partition-fragments-v2") {
		if err := os.RemoveAll(fragmentRoot); err != nil {
			return worldgraph.Manifest{}, err
		}
		shards, fragmentCounts, err := partitionGlobalFragments(
			ctx, waysPath, resolvedSortedPath, fragmentRoot, options.MaxOpenShards, DefaultMaxWayNodes,
		)
		if err != nil {
			return worldgraph.Manifest{}, err
		}
		if len(shards) == 0 {
			return worldgraph.Manifest{}, fmt.Errorf("global build produced no routable shards")
		}
		state.OccupiedShards = append([]worldgraph.TileID(nil), shards...)
		state.Counts.Shards = int64(len(shards))
		state.Counts.Segments = fragmentCounts.Segments
		state.Counts.Fragments = fragmentCounts.Fragments
		state.Counts.Contributions = fragmentCounts.Contributions
		if err := complete("partition-fragments-v2"); err != nil {
			return worldgraph.Manifest{}, err
		}
	}
	if state.hasStage("partition-fragments-v2") {
		if err := removeGlobalBuildArtifacts(resolvedSortedPath, waysPath); err != nil {
			return worldgraph.Manifest{}, err
		}
	}

	if len(state.OccupiedShards) == 0 {
		return worldgraph.Manifest{}, fmt.Errorf("global build state has no occupied shards")
	}
	if err := removeGlobalBuildScratchFiles(fragmentRoot, ".fragment-contributions-", ".db"); err != nil {
		return worldgraph.Manifest{}, err
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
	recorded := make(map[string]struct{}, len(state.PackedShards))
	for _, key := range state.PackedShards {
		if _, ok := occupied[key]; !ok {
			return worldgraph.Manifest{}, fmt.Errorf("%w: packed shard %q is not occupied", ErrGlobalBuildStateMismatch, key)
		}
		if _, duplicate := recorded[key]; duplicate {
			return worldgraph.Manifest{}, fmt.Errorf("%w: duplicate packed shard %q", ErrGlobalBuildStateMismatch, key)
		}
		recorded[key] = struct{}{}
	}

	// Synced shard files are their own recovery journal. Discover all valid
	// occupied shards in one O(N) pass so progress does not rewrite and rescan
	// the entire work/store tree after every shard.
	packed := make(map[string]struct{}, len(state.OccupiedShards))
	validPacked := make([]string, 0, len(state.OccupiedShards))
	state.Counts.Chunks, state.Counts.Edges = 0, 0
	for _, shard := range state.OccupiedShards {
		key := globalShardStateKey(shard)
		chunks, edges, inspectErr := stage.ValidateShard(ctx, shard)
		if inspectErr != nil {
			if err := ctx.Err(); err != nil {
				return worldgraph.Manifest{}, err
			}
			continue
		}
		state.Counts.Chunks += int64(chunks)
		state.Counts.Edges += edges
		validPacked = append(validPacked, key)
		packed[key] = struct{}{}
	}
	sort.Strings(validPacked)
	state.PackedShards = validPacked
	if len(packed) != len(state.OccupiedShards) {
		state.uncompleteStage("write-packs")
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
			spoolPath, err := fragmentSpoolPath(fragmentRoot, shard)
			if err != nil {
				return worldgraph.Manifest{}, err
			}
			builtChunks, builtEdges, err := buildPackedShardFromFragmentSpool(ctx, spoolPath, shard, stage.Path(), options.PackSegmentBytes)
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
			packed[key] = struct{}{}
			if options.Progress != nil {
				options.Progress(GlobalProgress{Stage: "write-packs", Counts: state.Counts, Elapsed: time.Since(started)})
			}
			if err := ctx.Err(); err != nil {
				return worldgraph.Manifest{}, err
			}
		}
		state.PackedShards = state.PackedShards[:0]
		for key := range packed {
			state.PackedShards = append(state.PackedShards, key)
		}
		sort.Strings(state.PackedShards)
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

func globalSortRecordLimit(memoryBytes int64, recordBytes int) int {
	if recordBytes < 1 {
		return 1
	}
	limit := memoryBytes / int64(recordBytes)
	maximumInt := int64(int(^uint(0) >> 1))
	if limit > maximumInt {
		limit = maximumInt
	}
	if limit < 1 {
		return 1
	}
	return int(limit)
}

func validateLegacyGlobalBuildArtifacts(waysPath, referencesPath, sortedReferencesPath, nodesPath string, counts GlobalCounts) error {
	ways, err := os.Stat(waysPath)
	if err != nil {
		return fmt.Errorf("%w: validate legacy way spool: %v", ErrGlobalBuildStateMismatch, err)
	}
	if !ways.Mode().IsRegular() {
		return fmt.Errorf("%w: legacy way spool is not a regular file", ErrGlobalBuildStateMismatch)
	}
	checks := []struct {
		path        string
		description string
		records     int64
		recordBytes int64
		required    bool
	}{
		{referencesPath, "raw references", counts.References, int64RecordBytes, false},
		{sortedReferencesPath, "sorted references", counts.Nodes, int64RecordBytes, false},
		{nodesPath, "selected nodes", counts.Nodes, globalNodeRecordBytes, true},
	}
	for _, check := range checks {
		if check.records < 0 || check.records > math.MaxInt64/check.recordBytes {
			return fmt.Errorf("%w: legacy %s count is invalid", ErrGlobalBuildStateMismatch, check.description)
		}
		info, err := os.Stat(check.path)
		if errors.Is(err, os.ErrNotExist) && !check.required {
			continue
		}
		if err != nil {
			return fmt.Errorf("%w: validate legacy %s: %v", ErrGlobalBuildStateMismatch, check.description, err)
		}
		expected := check.records * check.recordBytes
		if !info.Mode().IsRegular() || info.Size() != expected {
			return fmt.Errorf("%w: legacy %s size %d, want %d", ErrGlobalBuildStateMismatch, check.description, info.Size(), expected)
		}
	}
	return nil
}

func removeGlobalBuildTemporaryMatches(directory string, patterns ...string) error {
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(directory, pattern))
		if err != nil {
			return err
		}
		for _, path := range matches {
			if filepath.Dir(path) != filepath.Clean(directory) {
				return fmt.Errorf("global temporary path escaped work directory: %q", path)
			}
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeGlobalBuildScratchFiles(root, prefix, suffix string) error {
	if root == "" || prefix == "" || suffix == "" {
		return fmt.Errorf("global scratch cleanup parameters are required")
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), suffix) {
			return nil
		}
		return os.Remove(path)
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func removeGlobalBuildArtifacts(paths ...string) error {
	for _, path := range paths {
		if path == "" {
			return fmt.Errorf("global build cleanup path is empty")
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
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
