package worldgraph

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
)

const DefaultPackSegmentBytes int64 = 1 << 30

type packWriterOptions struct {
	MaxSegmentBytes int64
	CreateFile      func(string) (*os.File, error)
}

type shardPackWriter struct {
	ctx        context.Context
	shard      TileID
	prefix     string
	maxBytes   int64
	createFile func(string) (*os.File, error)

	file       *os.File
	segment    uint16
	offset     int64
	paths      []string
	entries    []shardIndexEntry
	hasLast    bool
	lastSlot   uint8
	closed     bool
	aborted    bool
	closeIndex shardIndex
}

func newShardPackWriter(ctx context.Context, root string, shard TileID, options packWriterOptions) (*shardPackWriter, error) {
	if ctx == nil {
		return nil, fmt.Errorf("pack writer context is nil")
	}
	prefix, err := shardPackPrefix(root, shard)
	if err != nil {
		return nil, err
	}
	if options.MaxSegmentBytes == 0 {
		options.MaxSegmentBytes = DefaultPackSegmentBytes
	}
	if options.MaxSegmentBytes < 1 {
		return nil, fmt.Errorf("pack segment size must be positive")
	}
	if options.CreateFile == nil {
		options.CreateFile = func(path string) (*os.File, error) {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return nil, err
			}
			return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		}
	}
	return &shardPackWriter{
		ctx:        ctx,
		shard:      shard,
		prefix:     prefix,
		maxBytes:   options.MaxSegmentBytes,
		createFile: options.CreateFile,
	}, nil
}

func (writer *shardPackWriter) Append(tile TileID, chunk Chunk) error {
	if writer.closed || writer.aborted {
		return fmt.Errorf("pack writer is closed")
	}
	if err := writer.ctx.Err(); err != nil {
		return writer.abortWith(err)
	}
	address, err := packedShardAddress(tile)
	if err != nil {
		return err
	}
	if address.Shard != writer.shard {
		return fmt.Errorf("%w: tile %+v belongs to shard %+v, not %+v", ErrInvalidChunk, tile, address.Shard, writer.shard)
	}
	if chunk.Tile != tile {
		return fmt.Errorf("%w: chunk tile %+v does not match append tile %+v", ErrInvalidChunk, chunk.Tile, tile)
	}
	if writer.hasLast && address.Slot <= writer.lastSlot {
		return fmt.Errorf("%w: shard slots must be appended in increasing order", ErrInvalidChunk)
	}

	var encoded bytes.Buffer
	if err := EncodeChunk(&encoded, chunk); err != nil {
		return err
	}
	record := encoded.Bytes()
	if int64(len(record)) > writer.maxBytes {
		return fmt.Errorf("%w: encoded chunk is %d bytes, pack segment limit %d", ErrInvalidChunk, len(record), writer.maxBytes)
	}
	if err := writer.ctx.Err(); err != nil {
		return writer.abortWith(err)
	}
	if writer.file == nil {
		if err := writer.openSegment(); err != nil {
			return writer.abortWith(err)
		}
	} else if writer.offset > writer.maxBytes-int64(len(record)) {
		if err := writer.closeSegment(); err != nil {
			return writer.abortWith(err)
		}
		if writer.segment == math.MaxUint16 {
			return writer.abortWith(fmt.Errorf("pack segment count exceeds %d", uint64(math.MaxUint16)+1))
		}
		writer.segment++
		writer.offset = 0
		if err := writer.openSegment(); err != nil {
			return writer.abortWith(err)
		}
	}

	offset := writer.offset
	if err := writeAll(writer.file, record); err != nil {
		return writer.abortWith(err)
	}
	if err := writer.ctx.Err(); err != nil {
		return writer.abortWith(err)
	}
	writer.offset += int64(len(record))
	writer.entries = append(writer.entries, shardIndexEntry{
		Slot:    address.Slot,
		Segment: writer.segment,
		Offset:  uint64(offset),
		Length:  uint32(len(record)),
		SHA256:  sha256.Sum256(record),
	})
	writer.lastSlot = address.Slot
	writer.hasLast = true
	return nil
}

func (writer *shardPackWriter) Close() (shardIndex, error) {
	if writer.aborted {
		return shardIndex{}, fmt.Errorf("pack writer was aborted")
	}
	if writer.closed {
		return cloneShardIndex(writer.closeIndex), nil
	}
	if err := writer.ctx.Err(); err != nil {
		return shardIndex{}, writer.abortWith(err)
	}
	if err := writer.closeSegment(); err != nil {
		return shardIndex{}, writer.abortWith(err)
	}
	writer.closed = true
	writer.closeIndex = shardIndex{Shard: writer.shard, Entries: append([]shardIndexEntry(nil), writer.entries...)}
	return cloneShardIndex(writer.closeIndex), nil
}

func (writer *shardPackWriter) Abort() error {
	if writer.aborted {
		return nil
	}
	writer.aborted = true
	var cleanup []error
	if writer.file != nil {
		if err := writer.file.Close(); err != nil {
			cleanup = append(cleanup, err)
		}
		writer.file = nil
	}
	for _, path := range writer.paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanup = append(cleanup, err)
		}
	}
	return errors.Join(cleanup...)
}

func (writer *shardPackWriter) openSegment() error {
	path := packSegmentPath(writer.prefix, writer.segment)
	file, err := writer.createFile(path)
	if err != nil {
		return err
	}
	if file == nil {
		return fmt.Errorf("pack file creator returned nil")
	}
	writer.file = file
	writer.paths = append(writer.paths, path)
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	return nil
}

func (writer *shardPackWriter) closeSegment() error {
	if writer.file == nil {
		return nil
	}
	file := writer.file
	writer.file = nil
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(syncErr, closeErr)
}

func (writer *shardPackWriter) abortWith(cause error) error {
	return errors.Join(cause, writer.Abort())
}

func cloneShardIndex(index shardIndex) shardIndex {
	index.Entries = append([]shardIndexEntry(nil), index.Entries...)
	return index
}

func shardPackPrefix(root string, shard TileID) (string, error) {
	if _, err := packedShardTile(shard, 0); err != nil {
		return "", err
	}
	return filepath.Join(root, "shards", strconv.Itoa(shard.X>>4), strconv.Itoa(shard.X), strconv.Itoa(shard.Y)), nil
}

// WritePackedShard writes sorted chunks and their immutable shard index beneath root.
func WritePackedShard(ctx context.Context, root string, shard TileID, chunks []Chunk, maxSegmentBytes int64) error {
	if ctx == nil {
		return fmt.Errorf("packed shard context is nil")
	}
	if len(chunks) == 0 {
		return fmt.Errorf("packed shard %+v has no chunks", shard)
	}
	position := 0
	return WritePackedShardFrom(ctx, root, shard, func() (Chunk, bool, error) {
		if position == len(chunks) {
			return Chunk{}, false, nil
		}
		chunk := chunks[position]
		position++
		return chunk, true, nil
	}, maxSegmentBytes)
}

// WritePackedShardFrom pulls sorted chunks one at a time and writes their immutable shard index beneath root.
// The source must return chunks in strictly increasing packed-shard slot order. A false result ends the stream.
// Each chunk is encoded and appended before the source is called for the next chunk.
func WritePackedShardFrom(
	ctx context.Context,
	root string,
	shard TileID,
	next func() (Chunk, bool, error),
	maxSegmentBytes int64,
) error {
	if ctx == nil {
		return fmt.Errorf("packed shard context is nil")
	}
	if next == nil {
		return fmt.Errorf("packed shard chunk source is nil")
	}
	writer, err := newShardPackWriter(ctx, root, shard, packWriterOptions{MaxSegmentBytes: maxSegmentBytes})
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = writer.Abort()
		}
	}()
	chunkCount := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk, ok, err := next()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if err := writer.Append(chunk.Tile, chunk); err != nil {
			return err
		}
		chunkCount++
	}
	if chunkCount == 0 {
		return fmt.Errorf("packed shard %+v has no chunks", shard)
	}
	index, err := writer.Close()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	prefix, err := shardPackPrefix(root, shard)
	if err != nil {
		return err
	}
	path := shardIndexPath(prefix)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		if !complete {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if err := encodeShardIndex(file, index); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	complete = true
	return nil
}

func shardIndexPath(prefix string) string {
	return prefix + ".idx"
}

func packSegmentPath(prefix string, segment uint16) string {
	return fmt.Sprintf("%s-%03d.pack", prefix, segment)
}

func readPackedChunk(ctx context.Context, prefix string, entry shardIndexEntry) (*Chunk, error) {
	if ctx == nil {
		return nil, fmt.Errorf("packed chunk context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if entry.Length == 0 || uint64(entry.Length) > uint64(maxEncodedChunkBytes) || entry.Offset > math.MaxInt64 || uint64(entry.Length) > uint64(math.MaxInt64)-entry.Offset {
		return nil, fmt.Errorf("%w: invalid indexed chunk range", ErrCorruptChunk)
	}
	file, err := os.Open(packSegmentPath(prefix, entry.Segment))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %v", ErrMissingChunk, err)
		}
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	end := entry.Offset + uint64(entry.Length)
	if !info.Mode().IsRegular() || end > uint64(info.Size()) {
		return nil, fmt.Errorf("%w: indexed chunk range exceeds pack segment", ErrCorruptChunk)
	}
	data := make([]byte, int(entry.Length))
	section := io.NewSectionReader(file, int64(entry.Offset), int64(entry.Length))
	if _, err := io.ReadFull(readerWithContext{ctx: ctx, reader: section}, data); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: read indexed chunk: %v", ErrCorruptChunk, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if sha256.Sum256(data) != entry.SHA256 {
		return nil, fmt.Errorf("%w: packed chunk digest mismatch", ErrCorruptChunk)
	}
	chunk, err := DecodeChunk(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: decode packed chunk: %w", ErrCorruptChunk, err)
	}
	return chunk, nil
}
