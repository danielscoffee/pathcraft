package worldgraph

import (
	"bytes"
	"encoding/binary"
	"errors"
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

	info, err := os.Stat(publishedChunkPath(dir, manifest.Generation, tiles[0]))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("chunk mode = %o, want 600", got)
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
