package worldgraph

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPackWriterSplitsAndIndexesChunks(t *testing.T) {
	root := t.TempDir()
	shard := TileID{Z: PackedShardZoom, X: 17, Y: 2}
	chunks := []Chunk{
		{Tile: mustPackedTile(t, shard, 0)},
		{Tile: mustPackedTile(t, shard, 1)},
		{Tile: mustPackedTile(t, shard, 2)},
	}
	encoded := make([][]byte, len(chunks))
	for index, chunk := range chunks {
		encoded[index] = mustEncodeChunk(t, chunk)
	}

	writer, err := newShardPackWriter(context.Background(), root, shard, packWriterOptions{
		MaxSegmentBytes: int64(len(encoded[0]) + len(encoded[1])),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range chunks {
		if err := writer.Append(chunk.Tile, chunk); err != nil {
			t.Fatal(err)
		}
	}
	index, err := writer.Close()
	if err != nil {
		t.Fatal(err)
	}

	if index.Shard != shard || len(index.Entries) != len(chunks) {
		t.Fatalf("Close() index = %+v", index)
	}
	wantSegments := []uint16{0, 0, 1}
	wantOffsets := []uint64{0, uint64(len(encoded[0])), 0}
	for position, entry := range index.Entries {
		if entry.Slot != uint8(position) || entry.Segment != wantSegments[position] || entry.Offset != wantOffsets[position] {
			t.Fatalf("entry %d = %+v", position, entry)
		}
		if entry.Length != uint32(len(encoded[position])) || entry.SHA256 != sha256.Sum256(encoded[position]) {
			t.Fatalf("entry %d does not describe encoded chunk", position)
		}
	}

	prefix, err := shardPackPrefix(root, shard)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := filepath.Join(root, "shards", "1", "17", "2")
	if prefix != wantPrefix {
		t.Fatalf("shardPackPrefix() = %q, want %q", prefix, wantPrefix)
	}
	for segment, want := range [][]byte{bytes.Join(encoded[:2], nil), encoded[2]} {
		path := packSegmentPath(prefix, uint16(segment))
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("segment %d bytes differ", segment)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Fatalf("segment %d mode = %o, want 600", segment, mode)
		}
	}
}

func TestWritePackedShardFromMatchesBatchBytes(t *testing.T) {
	shard := TileID{Z: PackedShardZoom, X: 17, Y: 2}
	chunks := []Chunk{
		{Tile: mustPackedTile(t, shard, 0)},
		{Tile: mustPackedTile(t, shard, 17)},
		{Tile: mustPackedTile(t, shard, 255)},
	}
	first := mustEncodeChunk(t, chunks[0])
	second := mustEncodeChunk(t, chunks[1])
	maxSegmentBytes := int64(len(first) + len(second))

	batchRoot := t.TempDir()
	if err := WritePackedShard(context.Background(), batchRoot, shard, chunks, maxSegmentBytes); err != nil {
		t.Fatal(err)
	}
	streamRoot := t.TempDir()
	position := 0
	if err := WritePackedShardFrom(context.Background(), streamRoot, shard, func() (Chunk, bool, error) {
		if position == len(chunks) {
			return Chunk{}, false, nil
		}
		chunk := chunks[position]
		position++
		return chunk, true, nil
	}, maxSegmentBytes); err != nil {
		t.Fatal(err)
	}
	if position != len(chunks) {
		t.Fatalf("source returned %d chunks, want %d", position, len(chunks))
	}

	batchFiles := readPackFileTree(t, batchRoot)
	streamFiles := readPackFileTree(t, streamRoot)
	if !reflect.DeepEqual(streamFiles, batchFiles) {
		t.Fatalf("streamed packed shard differs from batch output")
	}
}

func TestWritePackedShardFromSerializesBeforeFetchingNextChunk(t *testing.T) {
	root := t.TempDir()
	shard := TileID{Z: PackedShardZoom, X: 17, Y: 2}
	prefix, err := shardPackPrefix(root, shard)
	if err != nil {
		t.Fatal(err)
	}
	position := 0
	var previousSize int64
	next := func() (Chunk, bool, error) {
		if position > 0 {
			info, err := os.Stat(packSegmentPath(prefix, 0))
			if err != nil {
				return Chunk{}, false, err
			}
			if info.Size() <= previousSize {
				return Chunk{}, false, fmt.Errorf("pack size %d did not grow before source call %d", info.Size(), position+1)
			}
			previousSize = info.Size()
		}
		if position == packedShardSlots {
			return Chunk{}, false, nil
		}
		chunk := Chunk{Tile: mustPackedTile(t, shard, uint8(position))}
		position++
		return chunk, true, nil
	}
	if err := WritePackedShardFrom(context.Background(), root, shard, next, DefaultPackSegmentBytes); err != nil {
		t.Fatal(err)
	}
	if position != packedShardSlots {
		t.Fatalf("source returned %d chunks, want %d", position, packedShardSlots)
	}
	index, err := loadShardIndex(context.Background(), shardIndexPath(prefix))
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entries) != packedShardSlots {
		t.Fatalf("index has %d entries, want %d", len(index.Entries), packedShardSlots)
	}
}

func TestWritePackedShardFromCancellationRemovesArtifacts(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	shard := TileID{Z: PackedShardZoom, X: 3, Y: 4}
	position := 0
	err := WritePackedShardFrom(ctx, root, shard, func() (Chunk, bool, error) {
		if position == 1 {
			cancel()
		}
		chunk := Chunk{Tile: mustPackedTile(t, shard, uint8(position))}
		position++
		return chunk, true, nil
	}, DefaultPackSegmentBytes)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WritePackedShardFrom() error = %v, want context.Canceled", err)
	}
	assertNoPackArtifacts(t, root)
}

func TestWritePackedShardFromSourceFailureRemovesArtifacts(t *testing.T) {
	root := t.TempDir()
	shard := TileID{Z: PackedShardZoom, X: 3, Y: 4}
	failure := errors.New("source failed")
	position := 0
	err := WritePackedShardFrom(context.Background(), root, shard, func() (Chunk, bool, error) {
		if position == 1 {
			return Chunk{}, false, failure
		}
		chunk := Chunk{Tile: mustPackedTile(t, shard, 0)}
		position++
		return chunk, true, nil
	}, DefaultPackSegmentBytes)
	if !errors.Is(err, failure) {
		t.Fatalf("WritePackedShardFrom() error = %v, want source failure", err)
	}
	assertNoPackArtifacts(t, root)
}

func TestPackWriterRejectsInvalidOrder(t *testing.T) {
	shard := TileID{Z: PackedShardZoom, X: 3, Y: 4}
	tests := []struct {
		name   string
		first  uint8
		second uint8
	}{
		{name: "duplicate", first: 1, second: 1},
		{name: "descending", first: 2, second: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer, err := newShardPackWriter(context.Background(), t.TempDir(), shard, packWriterOptions{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = writer.Abort() })
			first := Chunk{Tile: mustPackedTile(t, shard, test.first)}
			if err := writer.Append(first.Tile, first); err != nil {
				t.Fatal(err)
			}
			second := Chunk{Tile: mustPackedTile(t, shard, test.second)}
			if err := writer.Append(second.Tile, second); !errors.Is(err, ErrInvalidChunk) {
				t.Fatalf("Append() error = %v, want ErrInvalidChunk", err)
			}
		})
	}
}

func TestPackWriterRejectsAnotherShard(t *testing.T) {
	shard := TileID{Z: PackedShardZoom, X: 3, Y: 4}
	writer, err := newShardPackWriter(context.Background(), t.TempDir(), shard, packWriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Abort() })
	other := TileID{Z: PackedShardZoom, X: 4, Y: 4}
	chunk := Chunk{Tile: mustPackedTile(t, other, 0)}
	if err := writer.Append(chunk.Tile, chunk); !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("Append() error = %v, want ErrInvalidChunk", err)
	}
}

func TestPackWriterCancellationRemovesSegments(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	shard := TileID{Z: PackedShardZoom, X: 3, Y: 4}
	writer, err := newShardPackWriter(ctx, root, shard, packWriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	chunk := Chunk{Tile: mustPackedTile(t, shard, 0)}
	if err := writer.Append(chunk.Tile, chunk); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := writer.Close(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close() error = %v, want context.Canceled", err)
	}
	assertNoPackFiles(t, root)
}

func TestPackWriterReportsSyncFailure(t *testing.T) {
	reader, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	shard := TileID{Z: PackedShardZoom, X: 3, Y: 4}
	writer, err := newShardPackWriter(context.Background(), t.TempDir(), shard, packWriterOptions{
		CreateFile: func(string) (*os.File, error) { return output, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	chunk := Chunk{Tile: mustPackedTile(t, shard, 0)}
	if err := writer.Append(chunk.Tile, chunk); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Close(); err == nil {
		t.Fatal("Close() error = nil, want sync failure")
	}
}

func TestPackReadValidatesIndexedRecord(t *testing.T) {
	chunk := Chunk{Tile: TileID{Z: GlobalRoutingZoom, X: 272, Y: 32}}
	encoded := mustEncodeChunk(t, chunk)
	digest := sha256.Sum256(encoded)
	entry := shardIndexEntry{Length: uint32(len(encoded)), SHA256: digest}

	t.Run("valid", func(t *testing.T) {
		prefix := writePackSegment(t, encoded)
		got, err := readPackedChunk(context.Background(), prefix, entry)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(*got, chunk) {
			t.Fatalf("readPackedChunk() = %+v, want %+v", *got, chunk)
		}
	})

	t.Run("wrong digest", func(t *testing.T) {
		prefix := writePackSegment(t, encoded)
		bad := entry
		bad.SHA256[0]++
		if _, err := readPackedChunk(context.Background(), prefix, bad); !errors.Is(err, ErrCorruptChunk) {
			t.Fatalf("readPackedChunk() error = %v, want ErrCorruptChunk", err)
		}
	})

	t.Run("truncated", func(t *testing.T) {
		prefix := writePackSegment(t, encoded[:len(encoded)-1])
		if _, err := readPackedChunk(context.Background(), prefix, entry); !errors.Is(err, ErrCorruptChunk) {
			t.Fatalf("readPackedChunk() error = %v, want ErrCorruptChunk", err)
		}
	})

	t.Run("range outside file", func(t *testing.T) {
		prefix := writePackSegment(t, encoded)
		bad := entry
		bad.Offset = 1
		if _, err := readPackedChunk(context.Background(), prefix, bad); !errors.Is(err, ErrCorruptChunk) {
			t.Fatalf("readPackedChunk() error = %v, want ErrCorruptChunk", err)
		}
	})

	t.Run("canceled", func(t *testing.T) {
		prefix := writePackSegment(t, encoded)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := readPackedChunk(ctx, prefix, entry); !errors.Is(err, context.Canceled) {
			t.Fatalf("readPackedChunk() error = %v, want context.Canceled", err)
		}
	})

	t.Run("malformed chunk", func(t *testing.T) {
		data := []byte("not a chunk")
		prefix := writePackSegment(t, data)
		bad := shardIndexEntry{Length: uint32(len(data)), SHA256: sha256.Sum256(data)}
		if _, err := readPackedChunk(context.Background(), prefix, bad); !errors.Is(err, ErrCorruptChunk) {
			t.Fatalf("readPackedChunk() error = %v, want ErrCorruptChunk", err)
		}
	})

	t.Run("missing segment", func(t *testing.T) {
		prefix := filepath.Join(t.TempDir(), "missing")
		if _, err := readPackedChunk(context.Background(), prefix, entry); !errors.Is(err, ErrMissingChunk) {
			t.Fatalf("readPackedChunk() error = %v, want ErrMissingChunk", err)
		}
	})
}

func TestPackEncodeChunkRejectsShortChecksumWrite(t *testing.T) {
	writer := &shortChecksumWriter{}
	if err := EncodeChunk(writer, Chunk{Tile: TileID{Z: GlobalRoutingZoom}}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("EncodeChunk() error = %v, want io.ErrShortWrite", err)
	}
}

type shortChecksumWriter struct {
	calls int
}

func (writer *shortChecksumWriter) Write(data []byte) (int, error) {
	writer.calls++
	if writer.calls == 3 {
		return 0, nil
	}
	return len(data), nil
}

func mustPackedTile(t *testing.T, shard TileID, slot uint8) TileID {
	t.Helper()
	tile, err := packedShardTile(shard, slot)
	if err != nil {
		t.Fatal(err)
	}
	return tile
}

func mustEncodeChunk(t *testing.T, chunk Chunk) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := EncodeChunk(&encoded, chunk); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func writePackSegment(t *testing.T, data []byte) string {
	t.Helper()
	prefix := filepath.Join(t.TempDir(), "shard")
	path := packSegmentPath(prefix, 0)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return prefix
}

func readPackFileTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[relative], err = os.ReadFile(path)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

func assertNoPackArtifacts(t *testing.T, root string) {
	t.Helper()
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if extension := filepath.Ext(path); extension == ".pack" || extension == ".idx" {
			t.Fatalf("packed shard artifact remains after cancellation: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func assertNoPackFiles(t *testing.T, root string) {
	t.Helper()
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if filepath.Ext(path) == ".pack" {
			t.Fatalf("pack file remains after cancellation: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
