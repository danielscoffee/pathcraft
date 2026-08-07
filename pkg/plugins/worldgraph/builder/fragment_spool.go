package builder

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	internalosm "github.com/danielscoffee/pathcraft/internal/osm"
	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

const (
	fragmentSpoolVersion        = uint32(1)
	maxFragmentSegments         = DefaultMaxWayNodes - 1
	fragmentSpoolSegmentBytes   = 2 * (8 + 8 + 8) // ID, longitude, and latitude for each endpoint.
	fragmentSpoolBufferBytes    = 64 << 10
	maxFragmentSpoolRecordBytes = 4 + 8 + 1 +
		2 + worldgraph.MaxEdgeHighwayBytes +
		2 + worldgraph.MaxEdgeNameBytes +
		4 + maxFragmentSegments*fragmentSpoolSegmentBytes
	fragmentSpoolMinimumRecordBytes = 4 + 8 + 1 + 2 + 2 + 4 + fragmentSpoolSegmentBytes
	fragmentPlanetSource            = "planet"
)

var (
	ErrCorruptFragmentSpool         = errors.New("worldgraph fragment spool is corrupt")
	ErrFragmentSpoolSummaryMismatch = errors.New("worldgraph fragment spool summary does not match")
)

type globalFragmentSegment struct {
	From worldgraph.Node
	To   worldgraph.Node
}

type globalWayFragment struct {
	WayID           int64
	Highway         string
	Name            string
	Direction       internalosm.Direction
	RestrictWalking bool
	RestrictDriving bool
	Segments        []globalFragmentSegment
}

type fragmentSpoolSummary struct {
	Shard   worldgraph.TileID
	Records int64
	Bytes   int64
	SHA256  string
}

type fragmentRadixPartition struct {
	path       string
	prefix     uint32
	prefixBits int
}

type fragmentRadixOutput struct {
	partition fragmentRadixPartition
	shard     worldgraph.TileID
	file      *os.File
	buffer    *bufio.Writer
	digest    hash.Hash
	final     bool
	records   int64
	bytes     int64
}

type fragmentSpoolWriter struct {
	ctx       context.Context
	root      string
	workRoot  string
	finalRoot string
	maxOpen   int

	journal       *os.File
	journalBuffer *bufio.Writer
	shards        map[worldgraph.TileID]struct{}
	summaries     []fragmentSpoolSummary
	failure       error
	closed        bool
	installed     bool

	// These counters make the descriptor bound observable in adversarial tests.
	// They are maintained by the single goroutine that owns the writer.
	openFiles      int
	openOutputs    int
	peakOpenFiles  int
	peakOpenOutput int
	finalSyncs     int
}

func newFragmentSpoolWriter(ctx context.Context, root string, maxOpen int) (*fragmentSpoolWriter, error) {
	if ctx == nil {
		return nil, fmt.Errorf("fragment spool context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if root == "" || maxOpen < 1 {
		return nil, fmt.Errorf("fragment spool root and positive file limit are required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}

	workRoot := filepath.Join(root, ".fragment-radix-work")
	finalPath := filepath.Join(root, "fragments")
	if _, err := os.Stat(workRoot); err == nil {
		// A fixed private work directory is a crash marker. A final tree beside
		// it cannot have been returned successfully, so both are safe to discard.
		if removeErr := os.RemoveAll(workRoot); removeErr != nil {
			return nil, removeErr
		}
		if removeErr := os.RemoveAll(finalPath); removeErr != nil {
			return nil, removeErr
		}
		if syncErr := syncFragmentSpoolRoot(root); syncErr != nil {
			return nil, syncErr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if _, err := os.Stat(finalPath); err == nil {
		return nil, fmt.Errorf("fragment spool tree already exists at %s", finalPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	finalRoot := filepath.Join(workRoot, "final")
	if err := os.MkdirAll(filepath.Join(finalRoot, "fragments"), 0o700); err != nil {
		_ = os.RemoveAll(workRoot)
		return nil, err
	}
	journalPath := filepath.Join(workRoot, "input.bucket")
	journal, err := os.OpenFile(journalPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = os.RemoveAll(workRoot)
		return nil, err
	}
	if err := journal.Chmod(0o600); err != nil {
		_ = journal.Close()
		_ = os.RemoveAll(workRoot)
		return nil, err
	}
	writer := &fragmentSpoolWriter{
		ctx: ctx, root: root, workRoot: workRoot, finalRoot: finalRoot, maxOpen: maxOpen,
		journal: journal, journalBuffer: bufio.NewWriterSize(journal, fragmentSpoolBufferBytes),
		shards: make(map[worldgraph.TileID]struct{}), openFiles: 1, peakOpenFiles: 1,
	}
	return writer, nil
}

func (writer *fragmentSpoolWriter) Add(shard worldgraph.TileID, fragment globalWayFragment) error {
	if writer == nil || writer.closed {
		return fmt.Errorf("fragment spool writer is closed")
	}
	if writer.failure != nil {
		return writer.failure
	}
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	key, err := fragmentSpoolShardKey(shard)
	if err != nil {
		writer.failure = err
		return err
	}
	record, err := encodeFragmentSpoolRecord(shard, fragment)
	if err != nil {
		writer.failure = err
		return err
	}
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	var keyBytes [2]byte
	binary.BigEndian.PutUint16(keyBytes[:], key)
	if err := writeFixedRecord(writer.journalBuffer, keyBytes[:]); err != nil {
		writer.failure = err
		return err
	}
	if err := writeFixedRecord(writer.journalBuffer, record); err != nil {
		writer.failure = err
		return err
	}
	writer.shards[shard] = struct{}{}
	return nil
}

func (writer *fragmentSpoolWriter) Close() error {
	if writer == nil || writer.closed {
		return nil
	}
	writer.closed = true
	flushErr := writer.journalBuffer.Flush()
	closeErr := writer.journal.Close()
	writer.openFiles--
	writer.journal, writer.journalBuffer = nil, nil

	err := errors.Join(writer.failure, flushErr, closeErr, writer.ctx.Err())
	if err == nil {
		journalPath := filepath.Join(writer.workRoot, "input.bucket")
		err = writer.partitionFragmentBucket(fragmentRadixPartition{path: journalPath})
	}
	if err == nil {
		err = writer.installFragmentTree()
	}
	if err != nil {
		writer.summaries = nil
		return errors.Join(err, writer.cleanupIncompleteFragmentTree())
	}
	return nil
}

func (writer *fragmentSpoolWriter) Shards() []worldgraph.TileID {
	if writer == nil {
		return nil
	}
	shards := make([]worldgraph.TileID, 0, len(writer.shards))
	for shard := range writer.shards {
		shards = append(shards, shard)
	}
	sort.Slice(shards, func(i, j int) bool {
		if shards[i].Z != shards[j].Z {
			return shards[i].Z < shards[j].Z
		}
		if shards[i].X != shards[j].X {
			return shards[i].X < shards[j].X
		}
		return shards[i].Y < shards[j].Y
	})
	return shards
}

func (writer *fragmentSpoolWriter) Summaries() []fragmentSpoolSummary {
	if writer == nil {
		return nil
	}
	return append([]fragmentSpoolSummary(nil), writer.summaries...)
}

func fragmentSpoolShardKey(shard worldgraph.TileID) (uint16, error) {
	if err := validatePackedBuilderShard(shard); err != nil {
		return 0, err
	}
	return uint16(shard.X<<8 | shard.Y), nil
}

func fragmentSpoolShardForKey(key uint16) worldgraph.TileID {
	return worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: int(key >> 8), Y: int(key & 0xff)}
}

func (writer *fragmentSpoolWriter) partitionFragmentBucket(partition fragmentRadixPartition) error {
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	remaining := 16 - partition.prefixBits
	if remaining <= 0 {
		return fmt.Errorf("fragment radix partition has invalid prefix width %d", partition.prefixBits)
	}
	splitBits := writer.fragmentRadixSplitBits(remaining)
	children, summaries, err := writer.splitFragmentBucket(partition, splitBits)
	if err != nil {
		return err
	}
	if err := os.Remove(partition.path); err != nil {
		return err
	}
	if partition.prefixBits+splitBits == 16 {
		writer.summaries = append(writer.summaries, summaries...)
		return nil
	}
	sort.Slice(children, func(i, j int) bool { return children[i].prefix < children[j].prefix })
	for _, child := range children {
		if err := writer.partitionFragmentBucket(child); err != nil {
			return err
		}
	}
	return nil
}

func (writer *fragmentSpoolWriter) fragmentRadixSplitBits(remaining int) int {
	limit := writer.maxOpen
	if limit > 64 {
		// More fan-out only increases buffer memory. Six bits gives the default
		// limit three stable sequential passes over the 16-bit shard key.
		limit = 64
	}
	bits := 0
	for 1<<(bits+1) <= limit && bits < remaining {
		bits++
	}
	if bits == 0 {
		// With a one-output limit, each binary child is selected by its own
		// sequential scan. Input plus one output are the only descriptors open.
		bits = 1
	}
	if bits > remaining {
		return remaining
	}
	return bits
}

func (writer *fragmentSpoolWriter) splitFragmentBucket(
	partition fragmentRadixPartition,
	splitBits int,
) ([]fragmentRadixPartition, []fragmentSpoolSummary, error) {
	fanout := 1 << splitBits
	if writer.maxOpen == 1 && fanout > 1 {
		var children []fragmentRadixPartition
		var summaries []fragmentSpoolSummary
		for selected := 0; selected < fanout; selected++ {
			selectedChildren, selectedSummaries, err := writer.distributeFragmentBucket(partition, splitBits, selected)
			if err != nil {
				return nil, nil, err
			}
			children = append(children, selectedChildren...)
			summaries = append(summaries, selectedSummaries...)
		}
		return children, summaries, nil
	}
	return writer.distributeFragmentBucket(partition, splitBits, -1)
}

// distributeFragmentBucket performs one sequential source scan. selected is -1
// for normal radix fan-out, or a single child when MaxOpenShards is one.
func (writer *fragmentSpoolWriter) distributeFragmentBucket(
	partition fragmentRadixPartition,
	splitBits int,
	selected int,
) ([]fragmentRadixPartition, []fragmentSpoolSummary, error) {
	if err := writer.ctx.Err(); err != nil {
		return nil, nil, err
	}
	input, err := os.Open(partition.path)
	if err != nil {
		return nil, nil, err
	}
	writer.openFiles++
	writer.observeFragmentOpenFiles()
	info, statErr := input.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		closeErr := input.Close()
		writer.openFiles--
		if statErr != nil {
			return nil, nil, errors.Join(statErr, closeErr)
		}
		return nil, nil, errors.Join(fmt.Errorf("fragment radix input is not a regular file"), closeErr)
	}

	newPrefixBits := partition.prefixBits + splitBits
	final := newPrefixBits == 16
	outputs := make(map[int]*fragmentRadixOutput)
	reader := bufio.NewReaderSize(input, fragmentSpoolBufferBytes)
	scratch := make([]byte, fragmentSpoolBufferBytes)
	processErr := writer.scanFragmentBucket(reader, scratch, partition, splitBits, selected, final, outputs)
	contextErr := writer.ctx.Err()
	inputCloseErr := input.Close()
	writer.openFiles--
	children, summaries, outputCloseErr := writer.closeFragmentRadixOutputs(outputs, processErr == nil && contextErr == nil)
	if err := errors.Join(processErr, contextErr, inputCloseErr, outputCloseErr); err != nil {
		return nil, nil, err
	}
	return children, summaries, nil
}

func (writer *fragmentSpoolWriter) scanFragmentBucket(
	reader *bufio.Reader,
	scratch []byte,
	partition fragmentRadixPartition,
	splitBits int,
	selected int,
	final bool,
	outputs map[int]*fragmentRadixOutput,
) error {
	var keyBytes [2]byte
	var lengthBytes [4]byte
	for {
		if err := writer.ctx.Err(); err != nil {
			return err
		}
		count, err := io.ReadFull(reader, keyBytes[:])
		if errors.Is(err, io.EOF) && count == 0 {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: truncated radix shard key", ErrCorruptFragmentSpool)
		}
		if _, err := io.ReadFull(reader, lengthBytes[:]); err != nil {
			return fmt.Errorf("%w: truncated radix record length", ErrCorruptFragmentSpool)
		}
		length := binary.BigEndian.Uint32(lengthBytes[:])
		if length < fragmentSpoolMinimumRecordBytes || uint64(length) > uint64(maxFragmentSpoolRecordBytes) {
			return fmt.Errorf("%w: invalid radix record length %d", ErrCorruptFragmentSpool, length)
		}
		key := binary.BigEndian.Uint16(keyBytes[:])
		if partition.prefixBits > 0 && uint32(key)>>(16-partition.prefixBits) != partition.prefix {
			return fmt.Errorf("%w: shard key escaped radix prefix", ErrCorruptFragmentSpool)
		}
		shift := 16 - (partition.prefixBits + splitBits)
		child := int(uint32(key)>>shift) & ((1 << splitBits) - 1)
		var output *fragmentRadixOutput
		if selected < 0 || child == selected {
			output = outputs[child]
			if output == nil {
				childPrefix := partition.prefix<<splitBits | uint32(child)
				output, err = writer.openFragmentRadixOutput(childPrefix, partition.prefixBits+splitBits, final)
				if err != nil {
					return err
				}
				outputs[child] = output
			}
			if !final {
				if err := output.write(keyBytes[:]); err != nil {
					return err
				}
			}
			if err := output.write(lengthBytes[:]); err != nil {
				return err
			}
		}
		if err := writer.copyFragmentRecordPayload(output, reader, scratch, int64(length)); err != nil {
			return err
		}
		if output != nil {
			if output.records == math.MaxInt64 {
				return fmt.Errorf("fragment spool record count exceeds int64")
			}
			output.records++
		}
	}
}

func (writer *fragmentSpoolWriter) copyFragmentRecordPayload(
	output *fragmentRadixOutput,
	reader io.Reader,
	scratch []byte,
	remaining int64,
) error {
	for remaining > 0 {
		if err := writer.ctx.Err(); err != nil {
			return err
		}
		amount := int64(len(scratch))
		if amount > remaining {
			amount = remaining
		}
		chunk := scratch[:int(amount)]
		if _, err := io.ReadFull(reader, chunk); err != nil {
			return fmt.Errorf("%w: truncated radix record payload", ErrCorruptFragmentSpool)
		}
		if output != nil {
			if err := output.write(chunk); err != nil {
				return err
			}
		}
		remaining -= amount
	}
	return nil
}

func (writer *fragmentSpoolWriter) openFragmentRadixOutput(
	prefix uint32,
	prefixBits int,
	final bool,
) (*fragmentRadixOutput, error) {
	partition := fragmentRadixPartition{prefix: prefix, prefixBits: prefixBits}
	var shard worldgraph.TileID
	if final {
		shard = fragmentSpoolShardForKey(uint16(prefix))
		path, err := fragmentSpoolPath(writer.finalRoot, shard)
		if err != nil {
			return nil, err
		}
		partition.path = path
	} else {
		partition.path = filepath.Join(writer.workRoot, "buckets", fmt.Sprintf("%02d-%04x.bucket", prefixBits, prefix))
	}
	if err := os.MkdirAll(filepath.Dir(partition.path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(partition.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	output := &fragmentRadixOutput{
		partition: partition, shard: shard, file: file,
		buffer: bufio.NewWriterSize(file, fragmentSpoolBufferBytes), final: final,
	}
	if final {
		output.digest = sha256.New()
	}
	writer.openFiles++
	writer.openOutputs++
	writer.observeFragmentOpenFiles()
	return output, nil
}

func (output *fragmentRadixOutput) write(data []byte) error {
	if err := writeFixedRecord(output.buffer, data); err != nil {
		return err
	}
	if output.final {
		_, _ = output.digest.Write(data)
		if output.bytes > math.MaxInt64-int64(len(data)) {
			return fmt.Errorf("fragment spool byte count exceeds int64")
		}
		output.bytes += int64(len(data))
	}
	return nil
}

func (writer *fragmentSpoolWriter) closeFragmentRadixOutputs(
	outputs map[int]*fragmentRadixOutput,
	commit bool,
) ([]fragmentRadixPartition, []fragmentSpoolSummary, error) {
	indexes := make([]int, 0, len(outputs))
	for index := range outputs {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	children := make([]fragmentRadixPartition, 0, len(indexes))
	summaries := make([]fragmentSpoolSummary, 0, len(indexes))
	var closeErrors []error
	for _, index := range indexes {
		output := outputs[index]
		flushErr := output.buffer.Flush()
		var syncErr error
		if output.final && commit && flushErr == nil {
			writer.finalSyncs++
			syncErr = output.file.Sync()
		}
		closeErr := output.file.Close()
		writer.openOutputs--
		writer.openFiles--
		if err := errors.Join(flushErr, syncErr, closeErr); err != nil {
			closeErrors = append(closeErrors, err)
			continue
		}
		if output.final {
			summaries = append(summaries, fragmentSpoolSummary{
				Shard: output.shard, Records: output.records, Bytes: output.bytes,
				SHA256: hex.EncodeToString(output.digest.Sum(nil)),
			})
		} else {
			children = append(children, output.partition)
		}
	}
	return children, summaries, errors.Join(closeErrors...)
}

func (writer *fragmentSpoolWriter) observeFragmentOpenFiles() {
	if writer.openFiles > writer.peakOpenFiles {
		writer.peakOpenFiles = writer.openFiles
	}
	if writer.openOutputs > writer.peakOpenOutput {
		writer.peakOpenOutput = writer.openOutputs
	}
}

func (writer *fragmentSpoolWriter) installFragmentTree() error {
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	finalFragments := filepath.Join(writer.finalRoot, "fragments")
	if err := syncFragmentSpoolDirectoryTree(finalFragments); err != nil {
		return err
	}
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	destination := filepath.Join(writer.root, "fragments")
	if err := os.Rename(finalFragments, destination); err != nil {
		return err
	}
	writer.installed = true
	// A cross-directory rename changes both parent directories. Sync both
	// before removing the crash marker that distinguishes an installed tree.
	if err := syncBuilderDirectory(writer.finalRoot); err != nil {
		return err
	}
	if err := syncBuilderDirectory(writer.root); err != nil {
		return err
	}
	if err := os.RemoveAll(writer.workRoot); err != nil {
		return err
	}
	if err := syncFragmentSpoolRoot(writer.root); err != nil {
		return err
	}
	sort.Slice(writer.summaries, func(i, j int) bool {
		left, right := writer.summaries[i].Shard, writer.summaries[j].Shard
		if left.X != right.X {
			return left.X < right.X
		}
		return left.Y < right.Y
	})
	return nil
}

func (writer *fragmentSpoolWriter) cleanupIncompleteFragmentTree() error {
	var cleanupErrors []error
	if writer.installed {
		if err := os.RemoveAll(filepath.Join(writer.root, "fragments")); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	if err := os.RemoveAll(writer.workRoot); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}
	if _, err := os.Stat(writer.root); err == nil {
		if err := syncFragmentSpoolRoot(writer.root); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	return errors.Join(cleanupErrors...)
}

func syncFragmentSpoolDirectoryTree(root string) error {
	var directories []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Slice(directories, func(i, j int) bool {
		leftDepth := strings.Count(filepath.Clean(directories[i]), string(os.PathSeparator))
		rightDepth := strings.Count(filepath.Clean(directories[j]), string(os.PathSeparator))
		if leftDepth != rightDepth {
			return leftDepth > rightDepth
		}
		return directories[i] < directories[j]
	})
	for _, directory := range directories {
		if err := syncBuilderDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func syncFragmentSpoolRoot(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	absolute = filepath.Clean(absolute)
	if err := syncBuilderDirectory(absolute); err != nil {
		return err
	}
	parent := filepath.Dir(absolute)
	if parent == absolute {
		return nil
	}
	// The parent makes a newly-created spool root durable. Ancestors above it
	// are owned by the caller and are not mutated by fragment installation.
	return syncBuilderDirectory(parent)
}

func summarizeFragmentSpool(ctx context.Context, path string, shard worldgraph.TileID) (fragmentSpoolSummary, error) {
	if ctx == nil {
		return fragmentSpoolSummary{}, fmt.Errorf("fragment spool summary context is nil")
	}
	if err := ctx.Err(); err != nil {
		return fragmentSpoolSummary{}, err
	}
	if err := validatePackedBuilderShard(shard); err != nil {
		return fragmentSpoolSummary{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return fragmentSpoolSummary{}, err
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		closeErr := file.Close()
		if statErr != nil {
			return fragmentSpoolSummary{}, errors.Join(statErr, closeErr)
		}
		return fragmentSpoolSummary{}, errors.Join(fmt.Errorf("fragment spool is not a regular file"), closeErr)
	}
	digest := sha256.New()
	counter := &fragmentSpoolSummaryReader{reader: io.TeeReader(file, digest)}
	var records int64
	var readErr error
	for {
		if err := ctx.Err(); err != nil {
			readErr = err
			break
		}
		_, ok, err := readFragmentSpoolRecord(counter, shard)
		if err != nil {
			readErr = err
			break
		}
		if !ok {
			break
		}
		if records == math.MaxInt64 {
			readErr = fmt.Errorf("fragment spool record count exceeds int64")
			break
		}
		records++
	}
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return fragmentSpoolSummary{}, err
	}
	if records == 0 {
		return fragmentSpoolSummary{}, fmt.Errorf("%w: fragment spool is empty", ErrCorruptFragmentSpool)
	}
	return fragmentSpoolSummary{
		Shard: shard, Records: records, Bytes: counter.bytes,
		SHA256: hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

type fragmentSpoolSummaryReader struct {
	reader io.Reader
	bytes  int64
}

func (reader *fragmentSpoolSummaryReader) Read(data []byte) (int, error) {
	count, err := reader.reader.Read(data)
	reader.bytes += int64(count)
	return count, err
}

func verifyFragmentSpoolSummary(ctx context.Context, path string, expected fragmentSpoolSummary) error {
	if err := validateFragmentSpoolSummary(expected); err != nil {
		return err
	}
	actual, err := summarizeFragmentSpool(ctx, path, expected.Shard)
	if err != nil {
		return err
	}
	return compareFragmentSpoolSummaries(actual, expected)
}

func validateFragmentSpoolSummary(summary fragmentSpoolSummary) error {
	if summary.Records < 1 || summary.Bytes < 1 {
		return fmt.Errorf("invalid fragment spool summary")
	}
	digest, err := hex.DecodeString(summary.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("invalid fragment spool summary SHA-256")
	}
	if err := validatePackedBuilderShard(summary.Shard); err != nil {
		return fmt.Errorf("invalid fragment spool summary shard: %w", err)
	}
	return nil
}

func compareFragmentSpoolSummaries(actual, expected fragmentSpoolSummary) error {
	if actual == expected {
		return nil
	}
	return fmt.Errorf("%w: shard %+v has %d records, %d bytes, %s; want %d, %d, %s",
		ErrFragmentSpoolSummaryMismatch, expected.Shard,
		actual.Records, actual.Bytes, actual.SHA256,
		expected.Records, expected.Bytes, expected.SHA256)
}

func fragmentSpoolPath(root string, shard worldgraph.TileID) (string, error) {
	if err := validatePackedBuilderShard(shard); err != nil {
		return "", err
	}
	return filepath.Join(root, "fragments", strconv.Itoa(shard.X>>4), strconv.Itoa(shard.X), strconv.Itoa(shard.Y)+".spool"), nil
}

func writeFragmentSpoolRecord(writer io.Writer, shard worldgraph.TileID, fragment globalWayFragment) error {
	if writer == nil {
		return fmt.Errorf("fragment spool writer is nil")
	}
	record, err := encodeFragmentSpoolRecord(shard, fragment)
	if err != nil {
		return err
	}
	return writeFixedRecord(writer, record)
}

func encodeFragmentSpoolRecord(shard worldgraph.TileID, fragment globalWayFragment) ([]byte, error) {
	if err := validateGlobalWayFragment(shard, fragment); err != nil {
		return nil, err
	}

	var payload bytes.Buffer
	payload.Grow(fragmentSpoolMinimumRecordBytes + len(fragment.Highway) + len(fragment.Name) + (len(fragment.Segments)-1)*fragmentSpoolSegmentBytes)
	if err := binary.Write(&payload, binary.BigEndian, fragmentSpoolVersion); err != nil {
		return nil, err
	}
	if err := writeFixedRecord(&payload, nodeKey(fragment.WayID)); err != nil {
		return nil, err
	}
	policy := uint8(fragment.Direction)
	if fragment.RestrictWalking {
		policy |= 1 << 2
	}
	if fragment.RestrictDriving {
		policy |= 1 << 3
	}
	if err := payload.WriteByte(policy); err != nil {
		return nil, err
	}
	if err := writeShortString(&payload, fragment.Highway); err != nil {
		return nil, err
	}
	if err := writeShortString(&payload, fragment.Name); err != nil {
		return nil, err
	}
	if err := binary.Write(&payload, binary.BigEndian, uint32(len(fragment.Segments))); err != nil {
		return nil, err
	}
	writeNode := func(node worldgraph.Node) error {
		if err := writeFixedRecord(&payload, nodeKey(node.ID)); err != nil {
			return err
		}
		if err := binary.Write(&payload, binary.BigEndian, math.Float64bits(node.Lon)); err != nil {
			return err
		}
		return binary.Write(&payload, binary.BigEndian, math.Float64bits(node.Lat))
	}
	for _, segment := range fragment.Segments {
		if err := writeNode(segment.From); err != nil {
			return nil, err
		}
		if err := writeNode(segment.To); err != nil {
			return nil, err
		}
	}
	if payload.Len() > maxFragmentSpoolRecordBytes {
		return nil, fmt.Errorf("way %d fragment spool payload exceeds %d bytes", fragment.WayID, maxFragmentSpoolRecordBytes)
	}

	record := make([]byte, 4+payload.Len())
	binary.BigEndian.PutUint32(record[:4], uint32(payload.Len()))
	copy(record[4:], payload.Bytes())
	return record, nil
}

func readFragmentSpoolRecord(reader io.Reader, shard worldgraph.TileID) (globalWayFragment, bool, error) {
	if reader == nil {
		return globalWayFragment{}, false, fmt.Errorf("fragment spool reader is nil")
	}
	if err := validatePackedBuilderShard(shard); err != nil {
		return globalWayFragment{}, false, err
	}

	var lengthBytes [4]byte
	count, err := io.ReadFull(reader, lengthBytes[:])
	if errors.Is(err, io.EOF) && count == 0 {
		return globalWayFragment{}, false, nil
	}
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return globalWayFragment{}, false, fmt.Errorf("%w: %w", ErrCorruptFragmentSpool, ErrTruncatedFixedRecord)
		}
		return globalWayFragment{}, false, err
	}
	length := binary.BigEndian.Uint32(lengthBytes[:])
	if length < fragmentSpoolMinimumRecordBytes || uint64(length) > uint64(maxFragmentSpoolRecordBytes) {
		return globalWayFragment{}, false, fmt.Errorf("%w: invalid record length %d", ErrCorruptFragmentSpool, length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return globalWayFragment{}, false, fmt.Errorf("%w: %w", ErrCorruptFragmentSpool, ErrTruncatedFixedRecord)
		}
		return globalWayFragment{}, false, err
	}

	decoded := bytes.NewReader(payload)
	var version uint32
	if err := binary.Read(decoded, binary.BigEndian, &version); err != nil || version != fragmentSpoolVersion {
		return globalWayFragment{}, false, fmt.Errorf("%w: unsupported version", ErrCorruptFragmentSpool)
	}
	var idBytes [8]byte
	if _, err := io.ReadFull(decoded, idBytes[:]); err != nil {
		return globalWayFragment{}, false, fmt.Errorf("%w: way ID", ErrCorruptFragmentSpool)
	}
	fragment := globalWayFragment{WayID: decodeNodeKey(idBytes[:])}
	policy, err := decoded.ReadByte()
	if err != nil || policy&^uint8(0x0f) != 0 || policy&3 == 3 {
		return globalWayFragment{}, false, fmt.Errorf("%w: invalid way policy", ErrCorruptFragmentSpool)
	}
	fragment.Direction = internalosm.Direction(policy & 3)
	fragment.RestrictWalking = policy&(1<<2) != 0
	fragment.RestrictDriving = policy&(1<<3) != 0
	fragment.Highway, err = readShortString(decoded, worldgraph.MaxEdgeHighwayBytes)
	if err != nil {
		return globalWayFragment{}, false, fmt.Errorf("%w: highway", ErrCorruptFragmentSpool)
	}
	fragment.Name, err = readShortString(decoded, worldgraph.MaxEdgeNameBytes)
	if err != nil {
		return globalWayFragment{}, false, fmt.Errorf("%w: name", ErrCorruptFragmentSpool)
	}
	var segmentCount uint32
	if err := binary.Read(decoded, binary.BigEndian, &segmentCount); err != nil || segmentCount == 0 || segmentCount > uint32(maxFragmentSegments) {
		return globalWayFragment{}, false, fmt.Errorf("%w: invalid segment count %d", ErrCorruptFragmentSpool, segmentCount)
	}
	if uint64(segmentCount)*fragmentSpoolSegmentBytes > uint64(decoded.Len()) {
		return globalWayFragment{}, false, fmt.Errorf("%w: truncated segments", ErrCorruptFragmentSpool)
	}
	fragment.Segments = make([]globalFragmentSegment, int(segmentCount))
	readNode := func() (worldgraph.Node, error) {
		if _, err := io.ReadFull(decoded, idBytes[:]); err != nil {
			return worldgraph.Node{}, err
		}
		var lonBits, latBits uint64
		if err := binary.Read(decoded, binary.BigEndian, &lonBits); err != nil {
			return worldgraph.Node{}, err
		}
		if err := binary.Read(decoded, binary.BigEndian, &latBits); err != nil {
			return worldgraph.Node{}, err
		}
		node := worldgraph.Node{
			ID: decodeNodeKey(idBytes[:]), Lon: math.Float64frombits(lonBits), Lat: math.Float64frombits(latBits),
		}
		if !validMercatorPosition(node.Lon, node.Lat) {
			return worldgraph.Node{}, fmt.Errorf("invalid Web Mercator coordinates")
		}
		owner, err := worldgraph.TileForPosition(node.Lon, node.Lat, worldgraph.GlobalRoutingZoom)
		if err != nil {
			return worldgraph.Node{}, err
		}
		node.Owner = owner
		return node, nil
	}
	for index := range fragment.Segments {
		fragment.Segments[index].From, err = readNode()
		if err != nil {
			return globalWayFragment{}, false, fmt.Errorf("%w: segment %d source: %v", ErrCorruptFragmentSpool, index, err)
		}
		fragment.Segments[index].To, err = readNode()
		if err != nil {
			return globalWayFragment{}, false, fmt.Errorf("%w: segment %d target: %v", ErrCorruptFragmentSpool, index, err)
		}
	}
	if decoded.Len() != 0 {
		return globalWayFragment{}, false, fmt.Errorf("%w: trailing record bytes", ErrCorruptFragmentSpool)
	}
	if err := validateGlobalWayFragment(shard, fragment); err != nil {
		return globalWayFragment{}, false, fmt.Errorf("%w: %v", ErrCorruptFragmentSpool, err)
	}
	return fragment, true, nil
}

func validateGlobalWayFragment(shard worldgraph.TileID, fragment globalWayFragment) error {
	if err := validatePackedBuilderShard(shard); err != nil {
		return err
	}
	if fragment.Highway == "" || len(fragment.Highway) > worldgraph.MaxEdgeHighwayBytes || len(fragment.Name) > worldgraph.MaxEdgeNameBytes {
		return fmt.Errorf("way %d fragment has invalid routing text", fragment.WayID)
	}
	if fragment.Direction != internalosm.DirectionBoth && fragment.Direction != internalosm.DirectionForward && fragment.Direction != internalosm.DirectionReverse {
		return fmt.Errorf("way %d fragment has invalid direction %d", fragment.WayID, fragment.Direction)
	}
	if len(fragment.Segments) == 0 || len(fragment.Segments) > maxFragmentSegments {
		return fmt.Errorf("way %d fragment has %d segments, limit 1..%d", fragment.WayID, len(fragment.Segments), maxFragmentSegments)
	}

	nodes := make(map[int64]worldgraph.Node)
	for index, segment := range fragment.Segments {
		if segment.From.ID == segment.To.ID {
			return fmt.Errorf("way %d fragment segment %d repeats node %d", fragment.WayID, index, segment.From.ID)
		}
		for _, node := range []worldgraph.Node{segment.From, segment.To} {
			if !validMercatorPosition(node.Lon, node.Lat) {
				return fmt.Errorf("way %d fragment node %d has invalid Web Mercator coordinates", fragment.WayID, node.ID)
			}
			owner, err := worldgraph.TileForPosition(node.Lon, node.Lat, worldgraph.GlobalRoutingZoom)
			if err != nil || owner != node.Owner {
				return fmt.Errorf("way %d fragment node %d has invalid owner", fragment.WayID, node.ID)
			}
			if previous, exists := nodes[node.ID]; exists && previous != node {
				return fmt.Errorf("way %d fragment has conflicting node %d", fragment.WayID, node.ID)
			}
			nodes[node.ID] = node
		}
		contributes, err := expandFragmentSegment(fragment, segment, shard, nil)
		if err != nil {
			return fmt.Errorf("way %d fragment segment %d: %w", fragment.WayID, index, err)
		}
		if !contributes {
			return fmt.Errorf("way %d fragment segment %d does not contribute to shard %+v", fragment.WayID, index, shard)
		}
	}
	return nil
}

func expandFragmentSegment(
	fragment globalWayFragment,
	segment globalFragmentSegment,
	shard worldgraph.TileID,
	consume func(edgeContribution) error,
) (bool, error) {
	way := Way{ID: fragment.WayID, Tags: map[string]string{"highway": fragment.Highway, "name": fragment.Name}}
	policy := internalosm.WayPolicy{
		Routable: true, Direction: fragment.Direction,
		RestrictWalking: fragment.RestrictWalking, RestrictDriving: fragment.RestrictDriving,
	}
	edges, err := segmentEdges(way, policy, segment.From, segment.To, fragmentPlanetSource, worldgraph.GlobalRoutingZoom)
	if err != nil {
		return false, err
	}
	if len(edges) != 2 {
		return false, fmt.Errorf("segment expansion produced %d directed edges, want 2", len(edges))
	}

	contributes := false
	for _, edge := range edges {
		from, to := segment.From, segment.To
		switch {
		case edge.ID.From == segment.From.ID && edge.ID.To == segment.To.ID:
		case edge.ID.From == segment.To.ID && edge.ID.To == segment.From.ID:
			from, to = to, from
		default:
			return false, fmt.Errorf("segment expansion produced invalid edge endpoints %+v", edge.ID)
		}
		for _, contribution := range edgeContributions(from, to, edge) {
			if err := validateEdgeContribution(contribution); err != nil {
				return false, err
			}
			actual, err := packedBuilderShard(contribution.tile)
			if err != nil {
				return false, err
			}
			if actual != shard {
				continue
			}
			contributes = true
			if consume != nil {
				if err := consume(contribution); err != nil {
					return false, err
				}
			}
		}
	}
	return contributes, nil
}

func replayFragmentSpool(ctx context.Context, path string, shard worldgraph.TileID, consume func(edgeContribution) error) error {
	return replayFragmentSpoolWithSummary(ctx, path, shard, nil, consume)
}

// replayFragmentSpoolVerified hashes and counts the bytes read during expansion,
// avoiding a separate verification scan. Verification finishes at EOF, so a
// caller must discard partial consumer effects when this function returns error.
func replayFragmentSpoolVerified(
	ctx context.Context,
	path string,
	expected fragmentSpoolSummary,
	consume func(edgeContribution) error,
) error {
	return replayFragmentSpoolWithSummary(ctx, path, expected.Shard, &expected, consume)
}

func replayFragmentSpoolWithSummary(
	ctx context.Context,
	path string,
	shard worldgraph.TileID,
	expected *fragmentSpoolSummary,
	consume func(edgeContribution) error,
) error {
	if ctx == nil {
		return fmt.Errorf("fragment spool replay context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validatePackedBuilderShard(shard); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("fragment spool consumer is nil")
	}
	if expected != nil {
		if expected.Shard != shard {
			return fmt.Errorf("fragment spool summary shard does not match replay shard")
		}
		if err := validateFragmentSpoolSummary(*expected); err != nil {
			return err
		}
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		closeErr := file.Close()
		closed = true
		if statErr != nil {
			return errors.Join(statErr, closeErr)
		}
		return errors.Join(fmt.Errorf("fragment spool is not a regular file"), closeErr)
	}

	var digest hash.Hash
	var counter *fragmentSpoolSummaryReader
	var reader io.Reader = file
	if expected != nil {
		digest = sha256.New()
		counter = &fragmentSpoolSummaryReader{reader: io.TeeReader(file, digest)}
		reader = counter
	}

	var records int64
	var replayErr error
replay:
	for {
		if err := ctx.Err(); err != nil {
			replayErr = err
			break
		}
		fragment, ok, err := readFragmentSpoolRecord(reader, shard)
		if err != nil {
			replayErr = err
			break
		}
		if !ok {
			break
		}
		if expected != nil {
			if records == math.MaxInt64 {
				replayErr = fmt.Errorf("fragment spool record count exceeds int64")
				break
			}
			records++
		}
		for _, segment := range fragment.Segments {
			if err := ctx.Err(); err != nil {
				replayErr = err
				break replay
			}
			contributes, err := expandFragmentSegment(fragment, segment, shard, func(contribution edgeContribution) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				return consume(contribution)
			})
			if err != nil {
				replayErr = err
				break replay
			}
			if !contributes {
				replayErr = fmt.Errorf("%w: segment does not contribute to shard %+v", ErrCorruptFragmentSpool, shard)
				break replay
			}
		}
	}
	closeErr := file.Close()
	closed = true
	if err := errors.Join(replayErr, closeErr); err != nil {
		return err
	}
	if expected == nil {
		return nil
	}
	if records == 0 {
		return fmt.Errorf("%w: fragment spool is empty", ErrCorruptFragmentSpool)
	}
	actual := fragmentSpoolSummary{
		Shard: shard, Records: records, Bytes: counter.bytes,
		SHA256: hex.EncodeToString(digest.Sum(nil)),
	}
	return compareFragmentSpoolSummaries(actual, *expected)
}

func buildPackedShardFromFragmentSpool(
	ctx context.Context,
	spoolPath string,
	shard worldgraph.TileID,
	stageRoot string,
	maxSegmentBytes int64,
) (int, int64, error) {
	if ctx == nil {
		return 0, 0, fmt.Errorf("packed fragment shard build context is nil")
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if err := validatePackedBuilderShard(shard); err != nil {
		return 0, 0, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(spoolPath), ".fragment-contributions-*.db")
	if err != nil {
		return 0, 0, err
	}
	databasePath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(databasePath)
		return 0, 0, err
	}
	if err := os.Remove(databasePath); err != nil {
		return 0, 0, err
	}
	defer os.Remove(databasePath)

	store, err := openContributionStore(databasePath)
	if err != nil {
		return 0, 0, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = store.Close()
		}
	}()
	batch := make([]edgeContribution, 0, contributionBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := store.PutBatch(ctx, batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	if err := replayFragmentSpool(ctx, spoolPath, shard, func(contribution edgeContribution) error {
		batch = append(batch, contribution)
		if len(batch) == contributionBatchSize {
			return flush()
		}
		return nil
	}); err != nil {
		return 0, 0, err
	}
	if err := flush(); err != nil {
		return 0, 0, err
	}

	tiles, err := store.Tiles(ctx)
	if err != nil {
		return 0, 0, err
	}
	width := 1 << (worldgraph.GlobalRoutingZoom - worldgraph.PackedShardZoom)
	sort.Slice(tiles, func(i, j int) bool {
		leftX, leftY := tiles[i].X&(width-1), tiles[i].Y&(width-1)
		rightX, rightY := tiles[j].X&(width-1), tiles[j].Y&(width-1)
		return leftY*width+leftX < rightY*width+rightX
	})
	position := 0
	var ownedEdges int64
	next := func() (worldgraph.Chunk, bool, error) {
		if position == len(tiles) {
			if err := store.Close(); err != nil {
				return worldgraph.Chunk{}, false, err
			}
			closed = true
			return worldgraph.Chunk{}, false, nil
		}
		tile := tiles[position]
		actual, err := packedBuilderShard(tile)
		if err != nil || actual != shard {
			return worldgraph.Chunk{}, false, fmt.Errorf("fragment spool produced tile %+v outside shard %+v", tile, shard)
		}
		chunk, found, err := store.Chunk(ctx, tile)
		if err != nil {
			return worldgraph.Chunk{}, false, err
		}
		if !found {
			return worldgraph.Chunk{}, false, fmt.Errorf("fragment contribution tile %+v disappeared", tile)
		}
		normalized := normalizeChunk(chunk)
		for _, edge := range normalized.Edges {
			if edge.Owner == tile {
				ownedEdges++
			}
		}
		position++
		return normalized, true, nil
	}
	if err := worldgraph.WritePackedShardFrom(ctx, stageRoot, shard, next, maxSegmentBytes); err != nil {
		return 0, 0, err
	}
	return position, ownedEdges, nil
}
