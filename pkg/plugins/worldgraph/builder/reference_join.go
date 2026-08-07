package builder

import (
	"bufio"
	"container/heap"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

const (
	wayNodeRequestRecordBytes  = 16
	resolvedWayNodeRecordBytes = 32
	globalJoinBufferBytes      = 4 << 20
)

var ErrCorruptReferenceJoin = errors.New("worldgraph reference join is corrupt")

type wayNodeRequest struct {
	NodeID     int64
	Occurrence uint64
}

type resolvedWayNode struct {
	Occurrence uint64
	NodeID     int64
	Lon        float64
	Lat        float64
}

func (record resolvedWayNode) node() worldgraph.Node {
	return worldgraph.Node{ID: record.NodeID, Lon: record.Lon, Lat: record.Lat}
}

func spoolWayNodeRequests(ctx context.Context, waySpoolPath, outputPath string, maxWayNodes int) (int64, error) {
	if ctx == nil {
		return 0, fmt.Errorf("way-node request context is nil")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if waySpoolPath == "" || outputPath == "" {
		return 0, fmt.Errorf("way-node request paths are required")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return 0, err
	}
	output, err := os.CreateTemp(filepath.Dir(outputPath), ".way-node-requests-*.tmp")
	if err != nil {
		return 0, err
	}
	temporary := output.Name()
	closed := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
		_ = os.Remove(temporary)
	}()
	if err := output.Chmod(0o600); err != nil {
		return 0, err
	}
	buffered := bufio.NewWriterSize(output, globalJoinBufferBytes)
	var occurrence uint64
	err = replayWaySpool(ctx, waySpoolPath, maxWayNodes, func(way Way) error {
		for _, nodeID := range way.NodeIDs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if occurrence >= math.MaxInt64 {
				return fmt.Errorf("way-node request count exceeds int64")
			}
			if err := writeWayNodeRequestRecord(buffered, wayNodeRequest{NodeID: nodeID, Occurrence: occurrence}); err != nil {
				return err
			}
			occurrence++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := buffered.Flush(); err != nil {
		return 0, err
	}
	if err := output.Sync(); err != nil {
		return 0, err
	}
	if err := output.Close(); err != nil {
		return 0, err
	}
	closed = true
	if err := os.Rename(temporary, outputPath); err != nil {
		return 0, err
	}
	if err := syncBuilderDirectory(filepath.Dir(outputPath)); err != nil {
		return 0, err
	}
	return int64(occurrence), nil
}

func writeWayNodeRequestRecord(writer io.Writer, record wayNodeRequest) error {
	var encoded [wayNodeRequestRecordBytes]byte
	copy(encoded[0:8], nodeKey(record.NodeID))
	binary.BigEndian.PutUint64(encoded[8:16], record.Occurrence)
	return writeFixedRecord(writer, encoded[:])
}

func readWayNodeRequestRecord(reader io.Reader) (wayNodeRequest, bool, error) {
	var encoded [wayNodeRequestRecordBytes]byte
	ok, err := readJoinFixedRecord(reader, encoded[:], "way-node request")
	if err != nil || !ok {
		return wayNodeRequest{}, ok, err
	}
	return wayNodeRequest{
		NodeID:     decodeNodeKey(encoded[0:8]),
		Occurrence: binary.BigEndian.Uint64(encoded[8:16]),
	}, true, nil
}

func writeResolvedWayNodeRecord(writer io.Writer, record resolvedWayNode) error {
	if !validOSMPosition(record.Lon, record.Lat) {
		return fmt.Errorf("resolved node %d has invalid coordinates", record.NodeID)
	}
	var encoded [resolvedWayNodeRecordBytes]byte
	binary.BigEndian.PutUint64(encoded[0:8], record.Occurrence)
	copy(encoded[8:16], nodeKey(record.NodeID))
	binary.BigEndian.PutUint64(encoded[16:24], math.Float64bits(record.Lon))
	binary.BigEndian.PutUint64(encoded[24:32], math.Float64bits(record.Lat))
	return writeFixedRecord(writer, encoded[:])
}

func readResolvedWayNodeRecord(reader io.Reader) (resolvedWayNode, bool, error) {
	var encoded [resolvedWayNodeRecordBytes]byte
	ok, err := readJoinFixedRecord(reader, encoded[:], "resolved way node")
	if err != nil || !ok {
		return resolvedWayNode{}, ok, err
	}
	record := resolvedWayNode{
		Occurrence: binary.BigEndian.Uint64(encoded[0:8]),
		NodeID:     decodeNodeKey(encoded[8:16]),
		Lon:        math.Float64frombits(binary.BigEndian.Uint64(encoded[16:24])),
		Lat:        math.Float64frombits(binary.BigEndian.Uint64(encoded[24:32])),
	}
	if !validOSMPosition(record.Lon, record.Lat) {
		return resolvedWayNode{}, false, fmt.Errorf("%w: resolved node %d has invalid coordinates", ErrCorruptReferenceJoin, record.NodeID)
	}
	return record, true, nil
}

func readJoinFixedRecord(reader io.Reader, encoded []byte, name string) (bool, error) {
	count, err := io.ReadFull(reader, encoded)
	if errors.Is(err, io.EOF) && count == 0 {
		return false, nil
	}
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, fmt.Errorf("%w: %s: %w", ErrCorruptReferenceJoin, name, ErrTruncatedFixedRecord)
		}
		return false, err
	}
	return true, nil
}

func sortWayNodeRequests(ctx context.Context, input, output, runDir string, maxRecords int) error {
	codec := fixedJoinCodec[wayNodeRequest]{
		read:  readWayNodeRequestRecord,
		write: writeWayNodeRequestRecord,
		less: func(left, right wayNodeRequest) bool {
			if left.NodeID != right.NodeID {
				return left.NodeID < right.NodeID
			}
			return left.Occurrence < right.Occurrence
		},
		runPattern: "request-*.run",
	}
	return sortFixedJoinRecords(ctx, input, output, runDir, maxRecords, codec)
}

func sortResolvedWayNodes(ctx context.Context, input, output, runDir string, maxRecords int) error {
	codec := fixedJoinCodec[resolvedWayNode]{
		read:  readResolvedWayNodeRecord,
		write: writeResolvedWayNodeRecord,
		less: func(left, right resolvedWayNode) bool {
			if left.Occurrence != right.Occurrence {
				return left.Occurrence < right.Occurrence
			}
			if left.NodeID != right.NodeID {
				return left.NodeID < right.NodeID
			}
			if math.Float64bits(left.Lon) != math.Float64bits(right.Lon) {
				return math.Float64bits(left.Lon) < math.Float64bits(right.Lon)
			}
			return math.Float64bits(left.Lat) < math.Float64bits(right.Lat)
		},
		runPattern: "resolved-*.run",
	}
	return sortFixedJoinRecords(ctx, input, output, runDir, maxRecords, codec)
}

type fixedJoinCodec[T any] struct {
	read       func(io.Reader) (T, bool, error)
	write      func(io.Writer, T) error
	less       func(T, T) bool
	runPattern string
}

func sortFixedJoinRecords[T any](ctx context.Context, input, output, runDir string, maxRecords int, codec fixedJoinCodec[T]) error {
	if ctx == nil {
		return fmt.Errorf("reference join sort context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if input == "" || output == "" || runDir == "" || codec.read == nil || codec.write == nil || codec.less == nil {
		return fmt.Errorf("reference join sort paths and codec are required")
	}
	if maxRecords < 1 {
		return fmt.Errorf("reference join sort record limit must be positive")
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return err
	}
	source, err := os.Open(input)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("reference join sort input is not a regular file")
	}
	bufferedSource := bufio.NewReaderSize(source, globalJoinBufferBytes)

	var temporaryPaths []string
	defer func() {
		for _, path := range temporaryPaths {
			_ = os.Remove(path)
		}
	}()
	newRunPath := func(pattern string) (string, error) {
		file, err := os.CreateTemp(runDir, pattern)
		if err != nil {
			return "", err
		}
		path := file.Name()
		if err := file.Close(); err != nil {
			_ = os.Remove(path)
			return "", err
		}
		if err := os.Remove(path); err != nil {
			return "", err
		}
		temporaryPaths = append(temporaryPaths, path)
		return path, nil
	}

	var runs []string
	batch := make([]T, 0, maxRecords)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		path, err := newRunPath(codec.runPattern)
		if err != nil {
			return err
		}
		sort.Slice(batch, func(i, j int) bool { return codec.less(batch[i], batch[j]) })
		if err := writeFixedJoinRun(ctx, path, batch, codec); err != nil {
			return err
		}
		runs = append(runs, path)
		batch = make([]T, 0, maxRecords)
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		record, ok, err := codec.read(bufferedSource)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		batch = append(batch, record)
		if len(batch) == maxRecords {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}

	for len(runs) > maxMergeFanIn {
		next := make([]string, 0, (len(runs)+maxMergeFanIn-1)/maxMergeFanIn)
		for start := 0; start < len(runs); start += maxMergeFanIn {
			end := min(start+maxMergeFanIn, len(runs))
			path, err := newRunPath("merge-*.run")
			if err != nil {
				return err
			}
			if err := mergeFixedJoinRuns(ctx, runs[start:end], path, codec); err != nil {
				return err
			}
			next = append(next, path)
			for _, consumed := range runs[start:end] {
				_ = os.Remove(consumed)
			}
		}
		runs = next
	}

	temporaryOutput, err := createEmptyJoinOutput(filepath.Dir(output))
	if err != nil {
		return err
	}
	defer os.Remove(temporaryOutput)
	if len(runs) == 0 {
		if err := syncRegularBuilderFile(temporaryOutput); err != nil {
			return err
		}
	} else {
		if err := os.Remove(temporaryOutput); err != nil {
			return err
		}
		if err := mergeFixedJoinRuns(ctx, runs, temporaryOutput, codec); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporaryOutput, output); err != nil {
		return err
	}
	return syncBuilderDirectory(filepath.Dir(output))
}

func createEmptyJoinOutput(directory string) (string, error) {
	file, err := os.CreateTemp(directory, ".join-sorted-*.tmp")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func writeFixedJoinRun[T any](ctx context.Context, path string, records []T, codec fixedJoinCodec[T]) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	buffered := bufio.NewWriterSize(file, globalJoinBufferBytes)
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := codec.write(buffered, record); err != nil {
			return err
		}
	}
	if err := buffered.Flush(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	return nil
}

type fixedJoinMergeItem[T any] struct {
	record T
	reader int
}

type fixedJoinMergeHeap[T any] struct {
	items []fixedJoinMergeItem[T]
	less  func(T, T) bool
}

func (items fixedJoinMergeHeap[T]) Len() int { return len(items.items) }
func (items fixedJoinMergeHeap[T]) Less(i, j int) bool {
	left, right := items.items[i], items.items[j]
	if items.less(left.record, right.record) {
		return true
	}
	if items.less(right.record, left.record) {
		return false
	}
	return left.reader < right.reader
}
func (items fixedJoinMergeHeap[T]) Swap(i, j int) {
	items.items[i], items.items[j] = items.items[j], items.items[i]
}
func (items *fixedJoinMergeHeap[T]) Push(value any) {
	items.items = append(items.items, value.(fixedJoinMergeItem[T]))
}
func (items *fixedJoinMergeHeap[T]) Pop() any {
	old := items.items
	last := old[len(old)-1]
	items.items = old[:len(old)-1]
	return last
}

func mergeFixedJoinRuns[T any](ctx context.Context, paths []string, output string, codec fixedJoinCodec[T]) error {
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	readers := make([]*os.File, len(paths))
	bufferedReaders := make([]*bufio.Reader, len(paths))
	defer func() {
		for _, reader := range readers {
			if reader != nil {
				_ = reader.Close()
			}
		}
	}()
	items := &fixedJoinMergeHeap[T]{less: codec.less}
	for index, path := range paths {
		reader, err := os.Open(path)
		if err != nil {
			return err
		}
		readers[index] = reader
		bufferedReaders[index] = bufio.NewReaderSize(reader, 64<<10)
		record, ok, err := codec.read(bufferedReaders[index])
		if err != nil {
			return err
		}
		if ok {
			heap.Push(items, fixedJoinMergeItem[T]{record: record, reader: index})
		}
	}
	bufferedOutput := bufio.NewWriterSize(file, globalJoinBufferBytes)
	for items.Len() > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := heap.Pop(items).(fixedJoinMergeItem[T])
		if err := codec.write(bufferedOutput, item.record); err != nil {
			return err
		}
		next, ok, err := codec.read(bufferedReaders[item.reader])
		if err != nil {
			return err
		}
		if ok {
			heap.Push(items, fixedJoinMergeItem[T]{record: next, reader: item.reader})
		}
	}
	if err := bufferedOutput.Flush(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	return nil
}

func joinWayNodeRequests(ctx context.Context, sortedRequestsPath, globalNodesPath, outputPath string) (int64, error) {
	if ctx == nil {
		return 0, fmt.Errorf("reference join context is nil")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	requests, err := os.Open(sortedRequestsPath)
	if err != nil {
		return 0, err
	}
	defer requests.Close()
	if info, err := requests.Stat(); err != nil {
		return 0, err
	} else if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("sorted way-node requests are not a regular file")
	}
	nodes, err := os.Open(globalNodesPath)
	if err != nil {
		return 0, err
	}
	defer nodes.Close()
	nodeCount, err := globalNodeRecordCount(globalNodesPath)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return 0, err
	}
	output, err := os.CreateTemp(filepath.Dir(outputPath), ".resolved-way-nodes-*.tmp")
	if err != nil {
		return 0, err
	}
	temporary := output.Name()
	closed := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
		_ = os.Remove(temporary)
	}()
	if err := output.Chmod(0o600); err != nil {
		return 0, err
	}
	requestReader := bufio.NewReaderSize(requests, globalJoinBufferBytes)
	nodeReader := bufio.NewReaderSize(nodes, globalJoinBufferBytes)
	outputWriter := bufio.NewWriterSize(output, globalJoinBufferBytes)

	var previousRequest wayNodeRequest
	hasPreviousRequest := false
	nextRequest := func() (wayNodeRequest, bool, error) {
		record, ok, err := readWayNodeRequestRecord(requestReader)
		if err != nil || !ok {
			return record, ok, err
		}
		if hasPreviousRequest && (record.NodeID < previousRequest.NodeID ||
			record.NodeID == previousRequest.NodeID && record.Occurrence <= previousRequest.Occurrence) {
			return wayNodeRequest{}, false, fmt.Errorf("%w: way-node requests are not strictly sorted", ErrCorruptReferenceJoin)
		}
		previousRequest = record
		hasPreviousRequest = true
		return record, true, nil
	}
	request, hasRequest, err := nextRequest()
	if err != nil {
		return 0, err
	}
	var previousNodeID int64
	var resolved int64
	for position := int64(0); position < nodeCount; position++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		node, err := readSequentialGlobalNodeRecord(nodeReader)
		if err != nil {
			return 0, err
		}
		if position > 0 && node.ID <= previousNodeID {
			return 0, fmt.Errorf("%w: global node IDs are not strictly sorted", ErrCorruptReferenceJoin)
		}
		previousNodeID = node.ID
		if hasRequest && request.NodeID < node.ID {
			return 0, fmt.Errorf("referenced node %d is missing from global node index", request.NodeID)
		}
		for hasRequest && request.NodeID == node.ID {
			if resolved == math.MaxInt64 {
				return 0, fmt.Errorf("resolved way-node count exceeds int64")
			}
			if err := writeResolvedWayNodeRecord(outputWriter, resolvedWayNode{
				Occurrence: request.Occurrence, NodeID: node.ID, Lon: node.Lon, Lat: node.Lat,
			}); err != nil {
				return 0, err
			}
			resolved++
			request, hasRequest, err = nextRequest()
			if err != nil {
				return 0, err
			}
		}
	}
	if hasRequest {
		return 0, fmt.Errorf("referenced node %d is missing from global node index", request.NodeID)
	}
	if err := outputWriter.Flush(); err != nil {
		return 0, err
	}
	if err := output.Sync(); err != nil {
		return 0, err
	}
	if err := output.Close(); err != nil {
		return 0, err
	}
	closed = true
	if err := os.Rename(temporary, outputPath); err != nil {
		return 0, err
	}
	if err := syncBuilderDirectory(filepath.Dir(outputPath)); err != nil {
		return 0, err
	}
	return resolved, nil
}

type resolvedWayNodeReader struct {
	file   *os.File
	reader *bufio.Reader
	next   uint64
	closed bool
}

func openResolvedWayNodeReader(path string) (*resolvedWayNodeReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() {
		return nil, errors.Join(fmt.Errorf("resolved way-node stream is not a regular file"), file.Close())
	}
	if info.Size()%resolvedWayNodeRecordBytes != 0 {
		return nil, errors.Join(fmt.Errorf("%w: resolved way-node stream size %d", ErrTruncatedFixedRecord, info.Size()), file.Close())
	}
	return &resolvedWayNodeReader{file: file, reader: bufio.NewReaderSize(file, globalJoinBufferBytes)}, nil
}

func (reader *resolvedWayNodeReader) Next() (resolvedWayNode, bool, error) {
	if reader == nil || reader.closed {
		return resolvedWayNode{}, false, fmt.Errorf("resolved way-node reader is closed")
	}
	record, ok, err := readResolvedWayNodeRecord(reader.reader)
	if err != nil || !ok {
		return record, ok, err
	}
	if record.Occurrence != reader.next {
		return resolvedWayNode{}, false, fmt.Errorf("%w: resolved occurrence %d, want %d", ErrCorruptReferenceJoin, record.Occurrence, reader.next)
	}
	if reader.next == math.MaxUint64 {
		return resolvedWayNode{}, false, fmt.Errorf("%w: resolved occurrence overflow", ErrCorruptReferenceJoin)
	}
	reader.next++
	return record, true, nil
}

func (reader *resolvedWayNodeReader) Close() error {
	if reader == nil || reader.closed {
		return nil
	}
	reader.closed = true
	return reader.file.Close()
}

func replayResolvedWayNodes(ctx context.Context, path string, consume func(resolvedWayNode) error) error {
	if ctx == nil {
		return fmt.Errorf("resolved way-node replay context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("resolved way-node consumer is nil")
	}
	reader, err := openResolvedWayNodeReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		record, ok, err := reader.Next()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := consume(record); err != nil {
			return err
		}
	}
}
