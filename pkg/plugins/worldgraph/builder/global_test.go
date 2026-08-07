package builder

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

func TestBuildGlobalResumesStagesAndPublishesPackedGeneration(t *testing.T) {
	pbf := writeGlobalTestPBF(t)
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	workDir := filepath.Join(root, "work")
	options := GlobalOptions{
		PBFPath: pbf, StorePath: storePath, WorkDir: workDir,
		RunMemoryBytes: 24, PackSegmentBytes: 1_024, MaxOpenShards: 2, Resume: true,
	}
	stages := []string{
		"validate-source",
		"spool-ways-and-refs",
		"sort-refs",
		"select-nodes",
		"spool-way-node-requests-v2",
		"sort-way-node-requests-v2",
		"join-way-node-requests-v2",
		"sort-resolved-way-nodes-v2",
		"partition-fragments-v2",
		"write-packs",
	}
	var stagedPackHashes map[string][sha256.Size]byte
	for _, stop := range stages {
		ctx, cancel := context.WithCancel(context.Background())
		options.Progress = func(progress GlobalProgress) {
			if progress.Stage == stop && progress.Completed {
				cancel()
			}
		}
		if _, err := BuildGlobal(ctx, options); !errors.Is(err, context.Canceled) {
			t.Fatalf("BuildGlobal(cancel after %s) error = %v, want context.Canceled", stop, err)
		}
		state, err := readGlobalBuildState(filepath.Join(workDir, globalBuildStateFilename))
		if err != nil {
			t.Fatal(err)
		}
		if !state.hasStage(stop) {
			t.Fatalf("stage %q not persisted: %+v", stop, state.Completed)
		}
		store, err := worldgraph.OpenStore(storePath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Manifest(); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("manifest visible after %s: %v", stop, err)
		}
		_ = store.Close()
		if stop == "write-packs" {
			stagedPackHashes = hashFilesWithExtension(t, state.StagePath, ".pack")
			if len(stagedPackHashes) < 3 {
				t.Fatalf("staged pack count = %d, want at least 3", len(stagedPackHashes))
			}
		}
	}

	options.Progress = nil
	manifest, err := BuildGlobal(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Layout != worldgraph.PackedLayout || manifest.Zoom != worldgraph.GlobalRoutingZoom ||
		manifest.ShardZoom != worldgraph.PackedShardZoom || len(manifest.Shards) != 3 || len(manifest.Tiles) != 0 ||
		len(manifest.Regions) != 1 || len(manifest.Regions[0].Tiles) != 0 {
		t.Fatalf("global manifest = %+v", manifest)
	}
	finalRoot := filepath.Join(storePath, "generations", manifest.Generation)
	if got := hashFilesWithExtension(t, finalRoot, ".pack"); !reflect.DeepEqual(got, stagedPackHashes) {
		t.Fatalf("final pack hashes = %v, want staged hashes %v", got, stagedPackHashes)
	}
	state, err := readGlobalBuildState(filepath.Join(workDir, globalBuildStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !state.hasStage("publish") || state.Counts.Shards != 3 || state.Counts.Chunks < 3 || state.Counts.Nodes != 6 ||
		state.Counts.Segments != 3 || state.Counts.Fragments != 3 || state.Counts.Contributions == 0 {
		t.Fatalf("final state = %+v", state)
	}
	workBytes, err := directoryBytes(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if state.Counts.WorkBytes != workBytes {
		t.Fatalf("checkpoint work bytes = %d, measured %d", state.Counts.WorkBytes, workBytes)
	}
	for _, obsolete := range []string{
		"ways.spool", "references.raw", "references.sorted", "nodes.idx",
		"way-node-requests.raw", "way-node-requests.sorted",
		"resolved-way-nodes.raw", "resolved-way-nodes.sorted",
	} {
		if _, err := os.Stat(filepath.Join(workDir, obsolete)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("obsolete work artifact %q remains: %v", obsolete, err)
		}
	}
	store, err := worldgraph.OpenStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, position := range [][2]float64{{12.56, 55.67}, {-34.90, -8.05}, {139.69, 35.68}} {
		tile, err := worldgraph.TileForPosition(position[0], position[1], worldgraph.GlobalRoutingZoom)
		if err != nil {
			t.Fatal(err)
		}
		covered, err := store.Covers(context.Background(), tile)
		if err != nil {
			t.Fatal(err)
		}
		if !covered {
			t.Fatalf("fixture tile %+v is not covered", tile)
		}
	}
}

func TestBuildGlobalPreservesPolarReferencesWithoutPublishingPolarEdges(t *testing.T) {
	pbf := writeGlobalFixturePBF(t, []globalFixtureNode{
		{id: 1, lon: 0, lat: 80},
		{id: 2, lon: 0, lat: 90},
		{id: 3, lon: 0.010, lat: 80},
		{id: 4, lon: 0.011, lat: 80},
	}, []globalFixtureWay{{id: 100, nodeIDs: []int64{1, 2, 3, 4}, name: "Polar"}})
	root := t.TempDir()
	manifest, err := BuildGlobal(context.Background(), GlobalOptions{
		PBFPath: pbf, StorePath: filepath.Join(root, "store"), WorkDir: filepath.Join(root, "work"),
		RunMemoryBytes: 24, PackSegmentBytes: 1_024, MaxOpenShards: 2, Resume: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Shards) != 1 {
		t.Fatalf("occupied shards = %v, want one", manifest.Shards)
	}
	state, err := readGlobalBuildState(filepath.Join(root, "work", globalBuildStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if state.Counts.Nodes != 4 || state.Counts.Edges != 2 {
		t.Fatalf("global counts = %+v, want four selected nodes and two published edges", state.Counts)
	}
	store, err := worldgraph.OpenStore(filepath.Join(root, "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tile, err := worldgraph.TileForPosition(0.010, 80, worldgraph.GlobalRoutingZoom)
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := store.LoadChunk(tile)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunk.Nodes) != 2 || chunk.Nodes[0].ID != 3 || chunk.Nodes[1].ID != 4 || len(chunk.Edges) != 2 {
		t.Fatalf("published polar-adjacent chunk = %+v", chunk)
	}
	for _, edge := range chunk.Edges {
		if edge.ID.From < 3 || edge.ID.To < 3 {
			t.Fatalf("published edge touches skipped polar segment: %+v", edge)
		}
	}
}

func TestGlobalPartitionV2MatchesLegacyPackedBytesWithSmallerSpool(t *testing.T) {
	root := t.TempDir()
	options := GlobalOptions{
		PBFPath: seamFixturePath(), StorePath: filepath.Join(root, "store"), WorkDir: filepath.Join(root, "work"),
		RunMemoryBytes: 64, PackSegmentBytes: 1 << 20, MaxOpenShards: 2, Resume: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	options.Progress = func(progress GlobalProgress) {
		if progress.Stage == "select-nodes" && progress.Completed {
			cancel()
		}
	}
	if _, err := BuildGlobal(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildGlobal() error = %v, want context.Canceled", err)
	}
	legacyWaysPath := filepath.Join(root, "legacy-ways.spool")
	legacyNodesPath := filepath.Join(root, "legacy-nodes.idx")
	for _, paths := range [][2]string{
		{filepath.Join(options.WorkDir, "ways.spool"), legacyWaysPath},
		{filepath.Join(options.WorkDir, "nodes.idx"), legacyNodesPath},
	} {
		data, err := os.ReadFile(paths[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths[1], data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	options.Progress = nil
	manifest, err := BuildGlobal(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	state, err := readGlobalBuildState(filepath.Join(options.WorkDir, globalBuildStateFilename))
	if err != nil {
		t.Fatal(err)
	}

	legacyRoot := filepath.Join(root, "legacy-contributions")
	index, err := openGlobalNodeIndex(legacyNodesPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyShards, legacyContributions, partitionErr := partitionGlobalContributions(
		context.Background(), legacyWaysPath, index, legacyRoot,
		fragmentPlanetSource, options.MaxOpenShards, DefaultMaxWayNodes,
	)
	closeErr := index.Close()
	if partitionErr != nil || closeErr != nil {
		t.Fatal(errors.Join(partitionErr, closeErr))
	}
	if !reflect.DeepEqual(legacyShards, state.OccupiedShards) {
		t.Fatalf("legacy shards = %v, v2 shards = %v", legacyShards, state.OccupiedShards)
	}
	if legacyContributions != state.Counts.Contributions {
		t.Fatalf("legacy contributions = %d, v2 logical contributions = %d", legacyContributions, state.Counts.Contributions)
	}
	fragmentBytes, err := directoryBytes(filepath.Join(options.WorkDir, "fragments-v2"))
	if err != nil {
		t.Fatal(err)
	}
	if fragmentBytes != state.Counts.FragmentBytes {
		t.Fatalf("fragment directory bytes = %d, state count = %d", fragmentBytes, state.Counts.FragmentBytes)
	}
	legacyBytes, err := directoryBytes(legacyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if fragmentBytes <= 0 || fragmentBytes >= legacyBytes {
		t.Fatalf("fragment bytes = %d, legacy bytes = %d; want compact fragments", fragmentBytes, legacyBytes)
	}

	legacyStage := filepath.Join(root, "legacy-stage")
	for _, shard := range legacyShards {
		spoolPath, err := shardSpoolPath(legacyRoot, shard)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := buildPackedShardFromSpool(context.Background(), spoolPath, shard, legacyStage, options.PackSegmentBytes); err != nil {
			t.Fatal(err)
		}
	}
	published := filepath.Join(options.StorePath, "generations", manifest.Generation)
	for _, extension := range []string{".idx", ".pack"} {
		want := hashFilesWithExtension(t, legacyStage, extension)
		if got := hashFilesWithExtension(t, published, extension); !reflect.DeepEqual(got, want) {
			t.Fatalf("v2 %s hashes = %v, legacy hashes = %v", extension, got, want)
		}
	}
}

func TestBuildGlobalValidatesArtifactsBeforeVersionOneUpgrade(t *testing.T) {
	pbf := writeGlobalTestPBF(t)
	root := t.TempDir()
	options := GlobalOptions{
		PBFPath: pbf, StorePath: filepath.Join(root, "store"), WorkDir: filepath.Join(root, "work"),
		RunMemoryBytes: 24, PackSegmentBytes: 1_024, MaxOpenShards: 2, Resume: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	options.Progress = func(progress GlobalProgress) {
		if progress.Stage == "select-nodes" && progress.Completed {
			cancel()
		}
	}
	if _, err := BuildGlobal(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildGlobal() error = %v, want context.Canceled", err)
	}
	statePath := filepath.Join(options.WorkDir, globalBuildStateFilename)
	state, err := readGlobalBuildState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	state.Version = 1
	if err := writeGlobalBuildState(statePath, state); err != nil {
		t.Fatal(err)
	}
	legacyResidue := filepath.Join(options.WorkDir, "contributions", "partial.spool")
	if err := os.MkdirAll(filepath.Dir(legacyResidue), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyResidue, []byte("uncheckpointed"), 0o600); err != nil {
		t.Fatal(err)
	}
	nodesPath := filepath.Join(options.WorkDir, "nodes.idx")
	info, err := os.Stat(nodesPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(nodesPath, info.Size()-globalNodeRecordBytes); err != nil {
		t.Fatal(err)
	}

	options.Progress = nil
	if _, err := BuildGlobal(context.Background(), options); !errors.Is(err, ErrGlobalBuildStateMismatch) {
		t.Fatalf("BuildGlobal() error = %v, want ErrGlobalBuildStateMismatch", err)
	}
	stored, err := readGlobalBuildState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != 1 || stored.hasStage("spool-way-node-requests-v2") {
		t.Fatalf("failed migration changed state = %+v", stored)
	}
	if _, err := os.Stat(legacyResidue); err != nil {
		t.Fatalf("legacy residue was removed before validation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(options.WorkDir, "way-node-requests.raw")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("request artifact exists after failed validation: %v", err)
	}
}

func TestBuildGlobalUpgradesValidatedVersionOneCheckpointAndCleansResidue(t *testing.T) {
	pbf := writeGlobalTestPBF(t)
	root := t.TempDir()
	options := GlobalOptions{
		PBFPath: pbf, StorePath: filepath.Join(root, "store"), WorkDir: filepath.Join(root, "work"),
		RunMemoryBytes: 24, PackSegmentBytes: 1_024, MaxOpenShards: 2, Resume: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	options.Progress = func(progress GlobalProgress) {
		if progress.Stage == "select-nodes" && progress.Completed {
			cancel()
		}
	}
	if _, err := BuildGlobal(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildGlobal() error = %v, want context.Canceled", err)
	}
	statePath := filepath.Join(options.WorkDir, globalBuildStateFilename)
	state, err := readGlobalBuildState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	state.Version = 1
	if err := writeGlobalBuildState(statePath, state); err != nil {
		t.Fatal(err)
	}
	legacyResidue := filepath.Join(options.WorkDir, "contributions", "partial.spool")
	if err := os.MkdirAll(filepath.Dir(legacyResidue), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyResidue, []byte("uncheckpointed"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	options.Progress = func(progress GlobalProgress) {
		if progress.Stage == "spool-way-node-requests-v2" && progress.Completed {
			cancel()
		}
	}
	if _, err := BuildGlobal(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("resumed BuildGlobal() error = %v, want context.Canceled", err)
	}
	stored, err := readGlobalBuildState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != globalBuildStateVersion || !stored.hasStage("spool-way-node-requests-v2") {
		t.Fatalf("migrated state = %+v", stored)
	}
	if _, err := os.Stat(filepath.Dir(legacyResidue)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy residue remains after migration: %v", err)
	}
}

func TestBuildGlobalRejectsCleanBoundaryFragmentLossBeforeShardRebuild(t *testing.T) {
	root := t.TempDir()
	options := GlobalOptions{
		PBFPath: seamFixturePath(), StorePath: filepath.Join(root, "store"), WorkDir: filepath.Join(root, "work"),
		RunMemoryBytes: 64, PackSegmentBytes: 1 << 20, MaxOpenShards: 2, Resume: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	options.Progress = func(progress GlobalProgress) {
		if progress.Stage == "partition-fragments-v2" && progress.Completed {
			cancel()
		}
	}
	if _, err := BuildGlobal(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildGlobal() error = %v, want context.Canceled", err)
	}
	state, err := readGlobalBuildState(filepath.Join(options.WorkDir, globalBuildStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.FragmentSpools) != 1 || state.FragmentSpools[0].Records < 2 {
		t.Fatalf("fragment spool summaries = %+v, want one multi-record spool", state.FragmentSpools)
	}
	spoolPath, err := fragmentSpoolPath(filepath.Join(options.WorkDir, "fragments-v2"), state.FragmentSpools[0].Shard)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(spoolPath)
	if err != nil {
		t.Fatal(err)
	}
	var lengthBytes [4]byte
	if _, err := io.ReadFull(file, lengthBytes[:]); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	firstRecordEnd := int64(4 + binary.BigEndian.Uint32(lengthBytes[:]))
	if firstRecordEnd >= state.FragmentSpools[0].Bytes {
		t.Fatalf("first record boundary = %d, spool bytes = %d", firstRecordEnd, state.FragmentSpools[0].Bytes)
	}
	if err := os.Truncate(spoolPath, firstRecordEnd); err != nil {
		t.Fatal(err)
	}

	options.Progress = nil
	if _, err := BuildGlobal(context.Background(), options); !errors.Is(err, ErrFragmentSpoolSummaryMismatch) {
		t.Fatalf("BuildGlobal() error = %v, want ErrFragmentSpoolSummaryMismatch", err)
	}
	store, err := worldgraph.OpenStore(options.StorePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Manifest(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest after fragment loss = %v, want not exist", err)
	}
}

func TestBuildGlobalRejectsLegacyUnpublishedPartitionState(t *testing.T) {
	root := t.TempDir()
	options := GlobalOptions{
		PBFPath: seamFixturePath(), StorePath: filepath.Join(root, "store"), WorkDir: filepath.Join(root, "work"),
		RunMemoryBytes: 64, PackSegmentBytes: 1 << 20, MaxOpenShards: 2, Resume: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	options.Progress = func(progress GlobalProgress) {
		if progress.Stage == "select-nodes" && progress.Completed {
			cancel()
		}
	}
	if _, err := BuildGlobal(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildGlobal() error = %v, want context.Canceled", err)
	}
	statePath := filepath.Join(options.WorkDir, globalBuildStateFilename)
	state, err := readGlobalBuildState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	state.completeStage("partition-contributions")
	if err := writeGlobalBuildState(statePath, state); err != nil {
		t.Fatal(err)
	}
	options.Progress = nil
	if _, err := BuildGlobal(context.Background(), options); !errors.Is(err, ErrGlobalBuildStateMismatch) {
		t.Fatalf("BuildGlobal() error = %v, want ErrGlobalBuildStateMismatch", err)
	}
}

func TestBuildGlobalRecoversUncheckpointedPartialShard(t *testing.T) {
	pbf := writeGlobalTestPBF(t)
	root := t.TempDir()
	options := GlobalOptions{
		PBFPath: pbf, StorePath: filepath.Join(root, "store"), WorkDir: filepath.Join(root, "work"),
		RunMemoryBytes: 24, PackSegmentBytes: 1_024, MaxOpenShards: 2, Resume: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	options.Progress = func(progress GlobalProgress) {
		if progress.Stage == "write-packs" && !progress.Completed {
			cancel()
		}
	}
	if _, err := BuildGlobal(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildGlobal() error = %v, want context.Canceled", err)
	}
	state, err := readGlobalBuildState(filepath.Join(options.WorkDir, globalBuildStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if state.StagePath == "" || len(state.OccupiedShards) == 0 || len(state.PackedShards) != 0 {
		t.Fatalf("interrupted state = %+v", state)
	}
	prefix := globalTestShardPrefix(state.StagePath, state.OccupiedShards[0])
	if err := os.MkdirAll(filepath.Dir(prefix), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prefix+"-000.pack", []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	options.Progress = nil
	if _, err := BuildGlobal(context.Background(), options); err != nil {
		t.Fatalf("resumed BuildGlobal() error = %v", err)
	}
}

func TestBuildGlobalRebuildsMissingCheckpointedShard(t *testing.T) {
	pbf := writeGlobalTestPBF(t)
	root := t.TempDir()
	options := GlobalOptions{
		PBFPath: pbf, StorePath: filepath.Join(root, "store"), WorkDir: filepath.Join(root, "work"),
		RunMemoryBytes: 24, PackSegmentBytes: 1_024, MaxOpenShards: 2, Resume: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	options.Progress = func(progress GlobalProgress) {
		if progress.Stage == "write-packs" && progress.Completed {
			cancel()
		}
	}
	if _, err := BuildGlobal(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildGlobal() error = %v, want context.Canceled", err)
	}
	before, err := readGlobalBuildState(filepath.Join(options.WorkDir, globalBuildStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !before.hasStage("write-packs") || len(before.PackedShards) != len(before.OccupiedShards) {
		t.Fatalf("checkpointed state = %+v", before)
	}
	prefix := globalTestShardPrefix(before.StagePath, before.OccupiedShards[0])
	if err := os.Remove(prefix + ".idx"); err != nil {
		t.Fatal(err)
	}
	options.Progress = nil
	if _, err := BuildGlobal(context.Background(), options); err != nil {
		t.Fatalf("resumed BuildGlobal() error = %v", err)
	}
	after, err := readGlobalBuildState(filepath.Join(options.WorkDir, globalBuildStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if after.Counts.Chunks != before.Counts.Chunks || after.Counts.Edges != before.Counts.Edges {
		t.Fatalf("recovered counts = %+v, want chunks %d edges %d", after.Counts, before.Counts.Chunks, before.Counts.Edges)
	}
}

func TestBuildGlobalRecoversGenerationRenameBeforeManifest(t *testing.T) {
	pbf := writeGlobalTestPBF(t)
	root := t.TempDir()
	options := GlobalOptions{
		PBFPath: pbf, StorePath: filepath.Join(root, "store"), WorkDir: filepath.Join(root, "work"),
		RunMemoryBytes: 24, PackSegmentBytes: 1_024, MaxOpenShards: 2, Resume: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	options.Progress = func(progress GlobalProgress) {
		if progress.Stage == "write-packs" && progress.Completed {
			cancel()
		}
	}
	if _, err := BuildGlobal(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildGlobal() error = %v, want context.Canceled", err)
	}
	state, err := readGlobalBuildState(filepath.Join(options.WorkDir, globalBuildStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(options.StorePath, "generations", state.Generation)
	if err := os.Rename(state.StagePath, target); err != nil {
		t.Fatal(err)
	}
	options.Progress = nil
	manifest, err := BuildGlobal(context.Background(), options)
	if err != nil {
		t.Fatalf("resumed BuildGlobal() error = %v", err)
	}
	if manifest.Generation != state.Generation {
		t.Fatalf("generation = %q, want %q", manifest.Generation, state.Generation)
	}
}

func globalTestShardPrefix(root string, shard worldgraph.TileID) string {
	return filepath.Join(root, "shards", strconv.Itoa(shard.X>>4), strconv.Itoa(shard.X), strconv.Itoa(shard.Y))
}

func TestBuildGlobalVerifiesSourceAgainBeforePublication(t *testing.T) {
	pbf := writeGlobalTestPBF(t)
	data, err := os.ReadFile(pbf)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	options := GlobalOptions{
		PBFPath: pbf, StorePath: filepath.Join(root, "store"), WorkDir: filepath.Join(root, "work"),
		RunMemoryBytes: 24, PackSegmentBytes: 1_024, MaxOpenShards: 2, Resume: true,
	}
	options.Progress = func(progress GlobalProgress) {
		if progress.Stage != "write-packs" || !progress.Completed {
			return
		}
		mutated := append([]byte(nil), data...)
		mutated[len(mutated)-1] ^= 0xff
		if err := os.WriteFile(pbf, mutated, 0o600); err != nil {
			t.Fatalf("mutate PBF: %v", err)
		}
	}
	if _, err := BuildGlobal(context.Background(), options); !errors.Is(err, ErrPBFSourceChanged) {
		t.Fatalf("BuildGlobal() error = %v, want ErrPBFSourceChanged", err)
	}
	store, err := worldgraph.OpenStore(options.StorePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Manifest(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest visible after source mutation: %v", err)
	}
}

func hashFilesWithExtension(t *testing.T, root, extension string) map[string][sha256.Size]byte {
	t.Helper()
	hashes := make(map[string][sha256.Size]byte)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != extension {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		hashes[relative] = sha256.Sum256(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return hashes
}

type globalFixtureNode struct {
	id       int64
	lon, lat float64
}

type globalFixtureWay struct {
	id      int64
	nodeIDs []int64
	name    string
}

func writeGlobalTestPBF(t *testing.T) string {
	t.Helper()
	return writeGlobalFixturePBF(t, []globalFixtureNode{
		{id: 1, lon: 12.56, lat: 55.67}, {id: 2, lon: 12.57, lat: 55.68},
		{id: 3, lon: -34.90, lat: -8.05}, {id: 4, lon: -34.89, lat: -8.04},
		{id: 5, lon: 139.69, lat: 35.68}, {id: 6, lon: 139.70, lat: 35.69},
	}, []globalFixtureWay{
		{id: 100, nodeIDs: []int64{1, 2}, name: "Copenhagen"},
		{id: 101, nodeIDs: []int64{3, 4}, name: "Recife"},
		{id: 102, nodeIDs: []int64{5, 6}, name: "Tokyo"},
	})
}

func writeGlobalFixturePBF(t *testing.T, nodes []globalFixtureNode, ways []globalFixtureWay) string {
	t.Helper()
	stringsTable := []string{"", "highway", "residential", "name"}
	stringIDs := map[string]uint64{"highway": 1, "residential": 2, "name": 3}
	for _, way := range ways {
		if _, exists := stringIDs[way.name]; exists {
			continue
		}
		stringIDs[way.name] = uint64(len(stringsTable))
		stringsTable = append(stringsTable, way.name)
	}
	var stringTable []byte
	for _, value := range stringsTable {
		stringTable = testPBFBytesField(stringTable, 1, []byte(value))
	}

	var ids, lats, lons []int64
	var previousID, previousLat, previousLon int64
	for _, node := range nodes {
		lat := int64(math.Round(node.lat * 1e7))
		lon := int64(math.Round(node.lon * 1e7))
		ids = append(ids, node.id-previousID)
		lats = append(lats, lat-previousLat)
		lons = append(lons, lon-previousLon)
		previousID, previousLat, previousLon = node.id, lat, lon
	}
	var dense []byte
	dense = testPBFBytesField(dense, 1, testPBFPackedSInt64(ids...))
	dense = testPBFBytesField(dense, 8, testPBFPackedSInt64(lats...))
	dense = testPBFBytesField(dense, 9, testPBFPackedSInt64(lons...))
	nodeGroup := testPBFBytesField(nil, 2, dense)

	var wayGroup []byte
	for _, way := range ways {
		encoded := testPBFVarintField(nil, 1, uint64(way.id))
		encoded = testPBFBytesField(encoded, 2, testPBFPackedVarints(stringIDs["highway"], stringIDs["name"]))
		encoded = testPBFBytesField(encoded, 3, testPBFPackedVarints(stringIDs["residential"], stringIDs[way.name]))
		references := make([]int64, len(way.nodeIDs))
		var previous int64
		for index, nodeID := range way.nodeIDs {
			references[index] = nodeID - previous
			previous = nodeID
		}
		encoded = testPBFBytesField(encoded, 8, testPBFPackedSInt64(references...))
		wayGroup = testPBFBytesField(wayGroup, 3, encoded)
	}
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 2, nodeGroup)
	block = testPBFBytesField(block, 2, wayGroup)
	path := writeTestPBF(t, testPBFFileBlock("OSMData", block))
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}
