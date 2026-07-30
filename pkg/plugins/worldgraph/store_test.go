package worldgraph

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestChunkRoundTrip(t *testing.T) {
	chunk := testChunk(t, 12.5683, 55.6761, "region-a")
	var encoded bytes.Buffer
	if err := EncodeChunk(&encoded, chunk); err != nil {
		t.Fatalf("EncodeChunk() error = %v", err)
	}
	got, err := DecodeChunk(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("DecodeChunk() error = %v", err)
	}
	if !reflect.DeepEqual(got, &chunk) {
		t.Fatalf("DecodeChunk() = %+v, want %+v", got, chunk)
	}
}

func TestDecodeChunkRejectsOversizedPayload(t *testing.T) {
	var header bytes.Buffer
	if _, err := io.WriteString(&header, chunkMagic); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(&header, binary.BigEndian, uint32(FormatVersion)); err != nil {
		t.Fatal(err)
	}
	header.Write(make([]byte, 32))
	reader := io.MultiReader(bytes.NewReader(header.Bytes()), io.LimitReader(zeroReader{}, MaxChunkPayloadBytes+1))
	if _, err := DecodeChunk(reader); !errors.Is(err, ErrCorruptChunk) {
		t.Fatalf("DecodeChunk() error = %v, want ErrCorruptChunk", err)
	}
}

func TestChunkRejectsStructuralLimits(t *testing.T) {
	chunk := testChunk(t, 12.5683, 55.6761, "region-a")
	chunk.Nodes = make([]Node, MaxChunkNodes+1)
	if err := chunk.validate(); !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("validate() node-limit error = %v, want ErrInvalidChunk", err)
	}
	chunk = testChunk(t, 12.5683, 55.6761, "region-a")
	chunk.Edges[0].Name = strings.Repeat("x", MaxEdgeNameBytes+1)
	if err := chunk.validate(); !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("validate() text-limit error = %v, want ErrInvalidChunk", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

func TestEdgeMidpointTileWrapsAntimeridian(t *testing.T) {
	from := Node{Lon: 179, Lat: 0}
	to := Node{Lon: -179, Lat: 0}
	want, err := TileForPosition(180, 0, DefaultZoom)
	if err != nil {
		t.Fatal(err)
	}
	forward, err := TileForEdge(from, to, DefaultZoom)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := TileForEdge(to, from, DefaultZoom)
	if err != nil {
		t.Fatal(err)
	}
	if forward != want || reverse != want {
		t.Fatalf("antimeridian midpoint tiles = %+v, %+v, want %+v", forward, reverse, want)
	}
}

func TestChunkRejectsIncorrectEdgeOwner(t *testing.T) {
	chunk := testChunk(t, 12.5683, 55.6761, "region-a")
	chunk.Edges[0].Owner.X++
	if err := EncodeChunk(&bytes.Buffer{}, chunk); !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("EncodeChunk() error = %v, want ErrInvalidChunk", err)
	}
}

func TestChunkRejectsChecksumMismatch(t *testing.T) {
	chunk := testChunk(t, 12.5683, 55.6761, "region-a")
	var encoded bytes.Buffer
	if err := EncodeChunk(&encoded, chunk); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	data[len(data)-1] ^= 0xff

	if _, err := DecodeChunk(bytes.NewReader(data)); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("DecodeChunk() error = %v, want ErrChecksumMismatch", err)
	}
}

func TestChunkRejectsUnsupportedVersion(t *testing.T) {
	chunk := testChunk(t, 12.5683, 55.6761, "region-a")
	var encoded bytes.Buffer
	if err := EncodeChunk(&encoded, chunk); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	binary.BigEndian.PutUint32(data[len(chunkMagic):], uint32(FormatVersion+1))

	if _, err := DecodeChunk(bytes.NewReader(data)); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("DecodeChunk() error = %v, want ErrUnsupportedVersion", err)
	}
}

func TestStoreDistinguishesUncoveredMissingAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	chunks := threeNeighborChunks(t)
	tiles := sortedChunkTiles(chunks)
	manifest := testManifest("generation-1", tiles)
	if err := store.PublishGeneration(manifest, chunks); err != nil {
		t.Fatalf("PublishGeneration() error = %v", err)
	}

	if _, err := store.LoadChunk(TileID{Z: tiles[0].Z, X: tiles[len(tiles)-1].X + 1, Y: tiles[0].Y}); !errors.Is(err, ErrUncoveredTile) {
		t.Fatalf("uncovered LoadChunk() error = %v, want ErrUncoveredTile", err)
	}

	missing := tiles[1]
	if err := os.Remove(publishedChunkPath(dir, manifest.Generation, missing)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadChunk(missing); !errors.Is(err, ErrMissingChunk) {
		t.Fatalf("missing LoadChunk() error = %v, want ErrMissingChunk", err)
	}

	corrupt := tiles[2]
	path := publishedChunkPath(dir, manifest.Generation, corrupt)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadChunk(corrupt); !errors.Is(err, ErrCorruptChunk) {
		t.Fatalf("corrupt LoadChunk() error = %v, want ErrCorruptChunk", err)
	}

	invalidProvenance := tiles[0]
	path = publishedChunkPath(dir, manifest.Generation, invalidProvenance)
	chunk := chunks[invalidProvenance]
	chunk.Edges[0].Sources = []string{"other-region"}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := writeChunkFile(path, chunk); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadChunk(invalidProvenance); !errors.Is(err, ErrCorruptChunk) {
		t.Fatalf("invalid-provenance LoadChunk() error = %v, want ErrCorruptChunk", err)
	}

	info, err := os.Stat(publishedChunkPath(dir, manifest.Generation, tiles[0]))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("chunk mode = %o, want 600", got)
	}
}

func TestPublishGenerationRejectsStaleStoreSnapshot(t *testing.T) {
	root := t.TempDir()
	first, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	chunk := testChunk(t, 0, 0, "region-a")
	if err := first.PublishGeneration(testManifest("generation-1", []TileID{chunk.Tile}), map[TileID]Chunk{chunk.Tile: chunk}); err != nil {
		t.Fatal(err)
	}
	if err := stale.PublishGeneration(testManifest("generation-2", []TileID{chunk.Tile}), map[TileID]Chunk{chunk.Tile: chunk}); !errors.Is(err, ErrPublishConflict) {
		t.Fatalf("stale PublishGeneration() error = %v, want ErrPublishConflict", err)
	}
	manifest, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	current, err := manifest.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != "generation-1" {
		t.Fatalf("current generation = %q, want generation-1", current.Generation)
	}
}

func TestPublishGenerationRejectsInvalidProvenance(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest, map[TileID]Chunk)
	}{
		{
			name: "no regions",
			mutate: func(manifest *Manifest, _ map[TileID]Chunk) {
				manifest.Regions = nil
			},
		},
		{
			name: "global tile without region",
			mutate: func(manifest *Manifest, _ map[TileID]Chunk) {
				manifest.Regions[0].Tiles = manifest.Regions[0].Tiles[:1]
			},
		},
		{
			name: "edge source absent from manifest",
			mutate: func(_ *Manifest, chunks map[TileID]Chunk) {
				for tile, chunk := range chunks {
					chunk.Edges[0].Sources = []string{"other-region"}
					chunks[tile] = chunk
					break
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			chunks := threeNeighborChunks(t)
			manifest := testManifest("generation-1", sortedChunkTiles(chunks))
			test.mutate(&manifest, chunks)
			if err := store.PublishGeneration(manifest, chunks); err == nil {
				t.Fatal("PublishGeneration() error = nil, want provenance rejection")
			}
		})
	}
}

func TestPublishGenerationRequiresChangedChunksWhenProvenanceChanges(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chunks := threeNeighborChunks(t)
	tiles := sortedChunkTiles(chunks)
	first := testManifest("generation-1", tiles)
	if err := store.PublishGeneration(first, chunks); err != nil {
		t.Fatal(err)
	}
	second := testManifest("generation-2", tiles)
	second.Regions[0].SourceSHA256 = strings.Repeat("b", 64)
	if err := store.PublishGeneration(second, nil); err == nil {
		t.Fatal("PublishGeneration() error = nil, want changed-chunk requirement")
	}
}

func TestPublishGenerationStreamsChangedChunks(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chunks := threeNeighborChunks(t)
	manifest := testManifest("generation-1", sortedChunkTiles(chunks))
	calls := 0
	if err := store.PublishGenerationFrom(manifest, func(tile TileID) (Chunk, bool, error) {
		calls++
		chunk, ok := chunks[tile]
		return chunk, ok, nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != len(manifest.Tiles) {
		t.Fatalf("chunk provider calls = %d, want %d", calls, len(manifest.Tiles))
	}
}

func TestPublishGenerationHonorsCancellationBeforeManifestCommit(t *testing.T) {
	root := t.TempDir()
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	chunks := threeNeighborChunks(t)
	first := testManifest("generation-1", sortedChunkTiles(chunks))
	if err := store.PublishGeneration(first, chunks); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manifestRenamed := false
	generations := filepath.Join(root, "generations")
	ops := publishFileOps{
		renameManifest: func(oldPath, newPath string) error {
			manifestRenamed = true
			return os.Rename(oldPath, newPath)
		},
		syncDirectory: func(path string) error {
			if path == generations {
				cancel()
			}
			return syncDirectory(path)
		},
	}
	second := testManifest("generation-2", sortedChunkTiles(chunks))
	if err := store.publishGenerationFromContextWithOps(ctx, second, chunkMapProvider(chunks), ops); !errors.Is(err, context.Canceled) {
		t.Fatalf("publish error = %v, want context.Canceled", err)
	}
	if manifestRenamed {
		t.Fatal("manifest committed after cancellation")
	}
	current, err := store.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != first.Generation {
		t.Fatalf("current generation = %q, want %q", current.Generation, first.Generation)
	}
	if _, err := os.Stat(filepath.Join(generations, second.Generation)); !os.IsNotExist(err) {
		t.Fatalf("canceled generation remains: %v", err)
	}
}

func TestPublishGenerationCommitWinsCancellationRace(t *testing.T) {
	root := t.TempDir()
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	chunks := threeNeighborChunks(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ops := publishFileOps{
		renameManifest: func(oldPath, newPath string) error {
			if err := os.Rename(oldPath, newPath); err != nil {
				return err
			}
			cancel()
			return nil
		},
		syncDirectory: syncDirectory,
	}
	manifest := testManifest("generation-1", sortedChunkTiles(chunks))
	if err := store.publishGenerationFromContextWithOps(ctx, manifest, chunkMapProvider(chunks), ops); err != nil {
		t.Fatalf("publish error after commit = %v, want success", err)
	}
	current, err := store.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != manifest.Generation {
		t.Fatalf("current generation = %q, want %q", current.Generation, manifest.Generation)
	}
}

func TestPublishGenerationSyncsDirectoriesAroundManifestRename(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	chunks := threeNeighborChunks(t)
	manifest := testManifest("generation-1", sortedChunkTiles(chunks))
	var events []string
	ops := publishFileOps{
		renameManifest: func(from, to string) error {
			events = append(events, "rename")
			return os.Rename(from, to)
		},
		syncDirectory: func(path string) error {
			switch path {
			case filepath.Join(dir, "generations"):
				events = append(events, "sync-generations")
			case dir:
				events = append(events, "sync-root")
			}
			return nil
		},
	}
	if err := store.publishGenerationWithOps(manifest, chunks, ops); err != nil {
		t.Fatal(err)
	}
	if want := []string{"sync-generations", "rename", "sync-root"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("publication events = %v, want %v", events, want)
	}
}

func TestPublishGenerationRollsBackAfterRootSyncFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	chunks := threeNeighborChunks(t)
	first := testManifest("generation-1", sortedChunkTiles(chunks))
	if err := store.PublishGeneration(first, chunks); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, manifestFilename))
	if err != nil {
		t.Fatal(err)
	}

	second := testManifest("generation-2", sortedChunkTiles(chunks))
	syncErr := errors.New("forced root sync failure")
	failed := false
	var pinned *Store
	ops := publishFileOps{
		renameManifest: os.Rename,
		syncDirectory: func(path string) error {
			if path == dir && !failed {
				failed = true
				var err error
				pinned, err = OpenStore(dir)
				if err != nil {
					return err
				}
				return syncErr
			}
			return syncDirectory(path)
		},
	}
	if err := store.publishGenerationWithOps(second, chunks, ops); !errors.Is(err, syncErr) {
		t.Fatalf("publishGenerationWithOps() error = %v, want %v", err, syncErr)
	}
	after, err := os.ReadFile(filepath.Join(dir, manifestFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("root sync failure changed current manifest")
	}
	current, err := store.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != first.Generation {
		t.Fatalf("current generation = %q, want %q", current.Generation, first.Generation)
	}
	if pinned == nil {
		t.Fatal("new generation was not pinned before rollback")
	}
	pinnedManifest, err := pinned.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if pinnedManifest.Generation != second.Generation {
		t.Fatalf("pinned generation = %q, want %q", pinnedManifest.Generation, second.Generation)
	}
	for tile := range chunks {
		if _, err := pinned.LoadChunk(tile); err != nil {
			t.Fatalf("pinned LoadChunk(%+v) error = %v", tile, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "generations", second.Generation)); err != nil {
		t.Fatalf("rolled-back generation missing: %v", err)
	}
}

func TestPublishGenerationPreservesPreviousManifestOnFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	firstChunks := threeNeighborChunks(t)
	first := testManifest("generation-1", sortedChunkTiles(firstChunks))
	if err := store.PublishGeneration(first, firstChunks); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, manifestFilename))
	if err != nil {
		t.Fatal(err)
	}

	secondChunks := threeNeighborChunks(t)
	second := testManifest("generation-2", sortedChunkTiles(secondChunks))
	renameErr := errors.New("forced manifest rename failure")
	if err := store.publishGeneration(second, secondChunks, func(_, _ string) error { return renameErr }); !errors.Is(err, renameErr) {
		t.Fatalf("publishGeneration() error = %v, want %v", err, renameErr)
	}
	after, err := os.ReadFile(filepath.Join(dir, manifestFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("failed publication changed current manifest")
	}
	current, err := store.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != first.Generation {
		t.Fatalf("current generation = %q, want %q", current.Generation, first.Generation)
	}
}

func TestPublishGenerationReusesUnchangedChunks(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	firstChunks := threeNeighborChunks(t)
	tiles := sortedChunkTiles(firstChunks)
	first := testManifest("generation-1", tiles)
	if err := store.PublishGeneration(first, firstChunks); err != nil {
		t.Fatal(err)
	}

	changed := firstChunks[tiles[0]]
	changed.Edges[0].Name = "changed"
	second := testManifest("generation-2", []TileID{tiles[2], tiles[0], tiles[1]})
	if err := store.PublishGeneration(second, map[TileID]Chunk{tiles[0]: changed}); err != nil {
		t.Fatalf("PublishGeneration(reuse) error = %v", err)
	}

	oldInfo, err := os.Stat(publishedChunkPath(dir, first.Generation, tiles[1]))
	if err != nil {
		t.Fatal(err)
	}
	newInfo, err := os.Stat(publishedChunkPath(dir, second.Generation, tiles[1]))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(oldInfo, newInfo) {
		t.Fatal("unchanged chunk was not hard-linked from previous generation")
	}
	current, err := store.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != second.Generation || !tilesSorted(current.Tiles) || !tilesSorted(current.Regions[0].Tiles) {
		t.Fatalf("published manifest not current and sorted: %+v", current)
	}
}

func testChunk(t *testing.T, lon, lat float64, source string) Chunk {
	t.Helper()
	tile, err := TileForPosition(lon, lat, DefaultZoom)
	if err != nil {
		t.Fatal(err)
	}
	bounds := tile.Bounds()
	lon1 := (bounds.West*2 + bounds.East) / 3
	lon2 := (bounds.West + bounds.East*2) / 3
	lat1 := (bounds.South*2 + bounds.North) / 3
	lat2 := (bounds.South + bounds.North*2) / 3
	return Chunk{
		Tile: tile,
		Nodes: []Node{
			{ID: 1, Lon: lon1, Lat: lat1, Owner: tile},
			{ID: 2, Lon: lon2, Lat: lat2, Owner: tile},
		},
		Edges: []Edge{{
			ID:             EdgeID{WayID: 10, From: 1, To: 2},
			DistanceMeters: 100,
			Highway:        "residential",
			Name:           "Main Street",
			Owner:          tile,
			Sources:        []string{source},
		}},
	}
}

func threeNeighborChunks(t *testing.T) map[TileID]Chunk {
	t.Helper()
	center := testChunk(t, 12.5683, 55.6761, "region-a")
	chunks := make(map[TileID]Chunk, 3)
	for dx := 0; dx < 3; dx++ {
		tile := center.Tile
		tile.X += dx
		chunk := center
		chunk.Tile = tile
		chunks[tile] = chunk
	}
	return chunks
}

func sortedChunkTiles(chunks map[TileID]Chunk) []TileID {
	tiles := make(map[TileID]struct{}, len(chunks))
	for tile := range chunks {
		tiles[tile] = struct{}{}
	}
	return sortedTileIDs(tiles)
}

func testManifest(generation string, tiles []TileID) Manifest {
	return Manifest{
		Generation: generation,
		Zoom:       DefaultZoom,
		BuiltAt:    time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		Regions: []RegionManifest{{
			Name:         "region-a",
			SourceSHA256: strings.Repeat("a", 64),
			Tiles:        append([]TileID(nil), tiles...),
		}},
		Tiles: append([]TileID(nil), tiles...),
	}
}

func publishedChunkPath(root, generation string, tile TileID) string {
	return filepath.Join(root, "generations", generation, tilePath(tile))
}

func tilesSorted(tiles []TileID) bool {
	for i := 1; i < len(tiles); i++ {
		previous, current := tiles[i-1], tiles[i]
		if previous.Z > current.Z ||
			(previous.Z == current.Z && previous.X > current.X) ||
			(previous.Z == current.Z && previous.X == current.X && previous.Y > current.Y) {
			return false
		}
	}
	return true
}
