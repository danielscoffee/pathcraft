package worldgraph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bbolt "go.etcd.io/bbolt"
)

func TestPublishPackedResumesAndCommitsManifestLast(t *testing.T) {
	root := t.TempDir()
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	shard := TileID{Z: PackedShardZoom, X: 17, Y: 2}
	tile := mustPackedTile(t, shard, 0)
	manifest := packedPublishTestManifest("packed-1", []TileID{shard})
	stage, err := store.BeginPackedGeneration(manifest)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := store.BeginPackedGeneration(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Path() != stage.Path() {
		t.Fatalf("resumed path = %q, want %q", resumed.Path(), stage.Path())
	}
	populatePackedStage(t, stage, map[TileID]Chunk{tile: {Tile: tile}})

	var events []string
	ops := defaultPackedPublishOps()
	ops.syncFile = func(path string) error {
		events = append(events, "file:"+filepath.Base(path))
		return syncRegularFile(path)
	}
	ops.syncDirectory = func(path string) error {
		events = append(events, "dir:"+path)
		return syncDirectory(path)
	}
	ops.renameGeneration = func(oldPath, newPath string) error {
		events = append(events, "rename-generation")
		if _, err := os.Stat(filepath.Join(root, manifestFilename)); !os.IsNotExist(err) {
			t.Fatalf("manifest visible before generation rename: %v", err)
		}
		return os.Rename(oldPath, newPath)
	}
	ops.renameManifest = func(oldPath, newPath string) error {
		events = append(events, "rename-manifest")
		if _, err := os.Stat(filepath.Join(root, "generations", manifest.Generation)); err != nil {
			t.Fatalf("generation missing before manifest rename: %v", err)
		}
		return os.Rename(oldPath, newPath)
	}
	if err := stage.commitWithOps(context.Background(), ops); err != nil {
		t.Fatal(err)
	}
	assertPackedPublishOrder(t, events, filepath.Join(root, "generations"), root)

	current, err := store.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != manifest.Generation || current.Layout != PackedLayout {
		t.Fatalf("Manifest() = %+v", current)
	}
	if _, err := os.Stat(stage.Path()); !os.IsNotExist(err) {
		t.Fatalf("stage remains after commit: %v", err)
	}
	if _, err := store.LoadChunk(tile); err != nil {
		t.Fatal(err)
	}
	if err := stage.Commit(context.Background()); err != nil {
		t.Fatalf("idempotent Commit() error = %v", err)
	}
}

func TestPublishPackedResumesWhenBuildTimeWasDefaulted(t *testing.T) {
	root := t.TempDir()
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manifest := packedPublishTestManifest("packed-default-time", []TileID{{Z: PackedShardZoom, X: 1, Y: 2}})
	manifest.BuiltAt = time.Time{}
	first, err := store.BeginPackedGeneration(manifest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.BeginPackedGeneration(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if second.Path() != first.Path() {
		t.Fatalf("resumed path = %q, want %q", second.Path(), first.Path())
	}
}

func TestPublishPackedCancellationPreservesResumableStage(t *testing.T) {
	root := t.TempDir()
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	shard := TileID{Z: PackedShardZoom, X: 17, Y: 2}
	tile := mustPackedTile(t, shard, 0)
	manifest := packedPublishTestManifest("packed-cancel", []TileID{shard})
	stage, err := store.BeginPackedGeneration(manifest)
	if err != nil {
		t.Fatal(err)
	}
	populatePackedStage(t, stage, map[TileID]Chunk{tile: {Tile: tile}})

	ctx, cancel := context.WithCancel(context.Background())
	ops := defaultPackedPublishOps()
	generations := filepath.Join(root, "generations")
	ops.syncDirectory = func(path string) error {
		if path == generations {
			cancel()
		}
		return syncDirectory(path)
	}
	if err := stage.commitWithOps(ctx, ops); !errors.Is(err, context.Canceled) {
		t.Fatalf("Commit() error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(stage.Path()); err != nil {
		t.Fatalf("resumable stage missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(generations, manifest.Generation)); !os.IsNotExist(err) {
		t.Fatalf("canceled target visible: %v", err)
	}
	if _, err := store.Manifest(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest after cancellation = %v, want os.ErrNotExist", err)
	}
	if err := stage.Commit(context.Background()); err != nil {
		t.Fatalf("resumed Commit() error = %v", err)
	}
}

func TestPublishPackedCommitWinsCancellationRace(t *testing.T) {
	store, stage, _, _ := packedStageFixture(t, "packed-race")
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	ops := defaultPackedPublishOps()
	ops.renameManifest = func(oldPath, newPath string) error {
		if err := os.Rename(oldPath, newPath); err != nil {
			return err
		}
		cancel()
		return nil
	}
	if err := stage.commitWithOps(ctx, ops); err != nil {
		t.Fatalf("Commit() after manifest rename = %v", err)
	}
	current, err := store.Manifest()
	if err != nil || current.Generation != "packed-race" {
		t.Fatalf("Manifest() = %+v, %v", current, err)
	}
}

func TestPublishPackedRejectsStaleStoreParent(t *testing.T) {
	root := t.TempDir()
	firstStore, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer firstStore.Close()
	staleStore, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer staleStore.Close()

	firstShard := TileID{Z: PackedShardZoom, X: 1, Y: 2}
	firstTile := mustPackedTile(t, firstShard, 0)
	first, err := firstStore.BeginPackedGeneration(packedPublishTestManifest("packed-first", []TileID{firstShard}))
	if err != nil {
		t.Fatal(err)
	}
	populatePackedStage(t, first, map[TileID]Chunk{firstTile: {Tile: firstTile}})

	staleShard := TileID{Z: PackedShardZoom, X: 2, Y: 2}
	staleTile := mustPackedTile(t, staleShard, 0)
	stale, err := staleStore.BeginPackedGeneration(packedPublishTestManifest("packed-stale", []TileID{staleShard}))
	if err != nil {
		t.Fatal(err)
	}
	populatePackedStage(t, stale, map[TileID]Chunk{staleTile: {Tile: staleTile}})

	if err := first.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := stale.Commit(context.Background()); !errors.Is(err, ErrPublishConflict) {
		t.Fatalf("stale Commit() error = %v, want ErrPublishConflict", err)
	}
	if _, err := os.Stat(stale.Path()); err != nil {
		t.Fatalf("stale stage removed: %v", err)
	}
}

func TestPublishPackedResumeRejectsChangedParent(t *testing.T) {
	root := t.TempDir()
	staleStore, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	staleShard := TileID{Z: PackedShardZoom, X: 1, Y: 2}
	staleManifest := packedPublishTestManifest("packed-resume-stale", []TileID{staleShard})
	if _, err := staleStore.BeginPackedGeneration(staleManifest); err != nil {
		t.Fatal(err)
	}
	if err := staleStore.Close(); err != nil {
		t.Fatal(err)
	}

	publisher, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	freshShard := TileID{Z: PackedShardZoom, X: 2, Y: 2}
	freshTile := mustPackedTile(t, freshShard, 0)
	fresh, err := publisher.BeginPackedGeneration(packedPublishTestManifest("packed-parent", []TileID{freshShard}))
	if err != nil {
		t.Fatal(err)
	}
	populatePackedStage(t, fresh, map[TileID]Chunk{freshTile: {Tile: freshTile}})
	if err := fresh.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.BeginPackedGeneration(staleManifest); !errors.Is(err, ErrPublishConflict) {
		t.Fatalf("BeginPackedGeneration() error = %v, want ErrPublishConflict", err)
	}
}

func TestPublishPackedRejectsLockContention(t *testing.T) {
	store, stage, _, root := packedStageFixture(t, "packed-lock")
	defer store.Close()
	lock, err := bbolt.Open(filepath.Join(root, ".publish.lock"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := stage.Commit(context.Background()); !errors.Is(err, ErrPublishConflict) {
		t.Fatalf("Commit() error = %v, want ErrPublishConflict", err)
	}
}

func TestPublishPackedRootSyncRollbackKeepsPinnedGeneration(t *testing.T) {
	root := t.TempDir()
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	firstShard := TileID{Z: PackedShardZoom, X: 1, Y: 2}
	firstTile := mustPackedTile(t, firstShard, 0)
	firstManifest := packedPublishTestManifest("packed-old", []TileID{firstShard})
	first, err := store.BeginPackedGeneration(firstManifest)
	if err != nil {
		t.Fatal(err)
	}
	populatePackedStage(t, first, map[TileID]Chunk{firstTile: {Tile: firstTile}})
	if err := first.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	secondShard := TileID{Z: PackedShardZoom, X: 2, Y: 2}
	secondTile := mustPackedTile(t, secondShard, 0)
	secondManifest := packedPublishTestManifest("packed-new", []TileID{secondShard})
	second, err := store.BeginPackedGeneration(secondManifest)
	if err != nil {
		t.Fatal(err)
	}
	populatePackedStage(t, second, map[TileID]Chunk{secondTile: {Tile: secondTile}})

	syncErr := errors.New("forced root sync failure")
	failed := false
	var pinned *Store
	ops := defaultPackedPublishOps()
	ops.syncDirectory = func(path string) error {
		if path == root && !failed {
			failed = true
			var err error
			pinned, err = OpenStore(root)
			if err != nil {
				return err
			}
			return syncErr
		}
		return syncDirectory(path)
	}
	if err := second.commitWithOps(context.Background(), ops); !errors.Is(err, syncErr) {
		t.Fatalf("Commit() error = %v, want %v", err, syncErr)
	}
	current, err := store.Manifest()
	if err != nil || current.Generation != firstManifest.Generation {
		t.Fatalf("current manifest = %+v, %v", current, err)
	}
	if pinned == nil {
		t.Fatal("new generation was not pinned")
	}
	defer pinned.Close()
	pinnedManifest, err := pinned.Manifest()
	if err != nil || pinnedManifest.Generation != secondManifest.Generation {
		t.Fatalf("pinned manifest = %+v, %v", pinnedManifest, err)
	}
	if _, err := pinned.LoadChunk(secondTile); err != nil {
		t.Fatalf("pinned LoadChunk() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "generations", secondManifest.Generation)); err != nil {
		t.Fatalf("pinned generation missing: %v", err)
	}
}

func TestPublishPackedRejectsUnexpectedStageFiles(t *testing.T) {
	t.Run("regular file", func(t *testing.T) {
		store, stage, _, _ := packedStageFixture(t, "packed-extra")
		defer store.Close()
		if err := os.WriteFile(filepath.Join(stage.Path(), "extra"), []byte("bad"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := stage.Commit(context.Background()); err == nil {
			t.Fatal("Commit() error = nil")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		store, stage, _, _ := packedStageFixture(t, "packed-symlink")
		defer store.Close()
		if err := os.Symlink("metadata.json", filepath.Join(stage.Path(), "link")); err != nil {
			t.Fatal(err)
		}
		if err := stage.Commit(context.Background()); err == nil {
			t.Fatal("Commit() error = nil")
		}
	})
}

func TestPublishPackedNeverOverwritesDestination(t *testing.T) {
	root := t.TempDir()
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manifest := packedPublishTestManifest("packed-existing", []TileID{{Z: PackedShardZoom}})
	target := filepath.Join(root, "generations", manifest.Generation)
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginPackedGeneration(manifest); err == nil {
		t.Fatal("BeginPackedGeneration() error = nil")
	}
}

func packedStageFixture(t *testing.T, generation string) (*Store, *PackedGenerationStage, TileID, string) {
	t.Helper()
	root := t.TempDir()
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	shard := TileID{Z: PackedShardZoom, X: 17, Y: 2}
	tile := mustPackedTile(t, shard, 0)
	stage, err := store.BeginPackedGeneration(packedPublishTestManifest(generation, []TileID{shard}))
	if err != nil {
		t.Fatal(err)
	}
	populatePackedStage(t, stage, map[TileID]Chunk{tile: {Tile: tile}})
	return store, stage, tile, root
}

func packedPublishTestManifest(generation string, shards []TileID) Manifest {
	return Manifest{
		Generation:    generation,
		Zoom:          GlobalRoutingZoom,
		Layout:        PackedLayout,
		LayoutVersion: PackedLayoutVersion,
		ShardZoom:     PackedShardZoom,
		Shards:        append([]TileID(nil), shards...),
		SourceBytes:   1234,
		BuiltAt:       time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
		Regions: []RegionManifest{{
			Name:         "planet",
			SourceSHA256: strings.Repeat("a", 64),
		}},
	}
}

func populatePackedStage(t *testing.T, stage *PackedGenerationStage, chunks map[TileID]Chunk) {
	t.Helper()
	groups := make(map[TileID][]Chunk)
	for tile, chunk := range chunks {
		address, err := packedShardAddress(tile)
		if err != nil {
			t.Fatal(err)
		}
		groups[address.Shard] = append(groups[address.Shard], chunk)
	}
	for shard, shardChunks := range groups {
		writer, err := newShardPackWriter(context.Background(), stage.Path(), shard, packWriterOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, chunk := range shardChunks {
			if err := writer.Append(chunk.Tile, chunk); err != nil {
				t.Fatal(err)
			}
		}
		index, err := writer.Close()
		if err != nil {
			t.Fatal(err)
		}
		prefix, err := shardPackPrefix(stage.Path(), shard)
		if err != nil {
			t.Fatal(err)
		}
		writeTestShardIndex(t, shardIndexPath(prefix), index)
	}
}

func assertPackedPublishOrder(t *testing.T, events []string, generations, root string) {
	t.Helper()
	find := func(value string) int {
		for index, event := range events {
			if event == value {
				return index
			}
		}
		return -1
	}
	renameGeneration := find("rename-generation")
	syncGenerations := find("dir:" + generations)
	renameManifest := find("rename-manifest")
	syncRoot := find("dir:" + root)
	if renameGeneration < 1 || syncGenerations <= renameGeneration || renameManifest <= syncGenerations || syncRoot <= renameManifest {
		t.Fatalf("publish order = %v", events)
	}
	for index, event := range events[:renameGeneration] {
		if index == 0 && !strings.HasPrefix(event, "file:") {
			t.Fatalf("files not synced first: %v", events)
		}
	}
}
