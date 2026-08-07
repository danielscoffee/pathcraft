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
	joinBufferBytes            = 256 << 10
)

type wayNodeRequest struct {
	NodeID     int64
	Occurrence uint64
}

type resolvedWayNode struct {
	Occurrence uint64
	Node       worldgraph.Node
}

// spoolWayNodeRequests gives every reference a stable position in ways.spool.
// That position lets later external-sort passes restore exact way order without
// retaining the planet-wide node set in memory.
func spoolWayNodeRequests(ctx context.Context, waySpoolPath, outputPath string, maxWayNodes int) (count int64, err error) {
	if ctx == nil {
		return 0, fmt.Errorf("way node request spool context is nil")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if waySpoolPath == "" || outputPath == "" {
		return 0, fmt.Errorf("way node request spool paths are required")
	}

	err = writeAtomicBuilderOutput(ctx, outputPath, ".way-node-requests-*.tmp", func(writer io.Writer) error {
		return replayWaySpool(ctx, waySpoolPath, maxWayNodes, func(way Way) error {
			for _, nodeID := range way.NodeIDs {
				if err := ctx.Err(); err != nil {
					return err
				}
				if count == math.MaxInt64 {
					return fmt.Errorf("way node request count exceeds int64")
				}
				request := wayNodeRequest{NodeID: nodeID, Occurrence: uint64(count)}
				if err := writeWayNodeRequestRecord(writer, request); err != nil {
					return err
				}
				count++
			}
			return nil
		})
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func sortWayNodeRequests(ctx context.Context, input, output, runDir string, maxRecords int) error {
	return sortFixedRecordFile(ctx, input, output, runDir, maxRecords, fixedRecordSortConfig[wayNodeRequest]{
		runPattern:  "way-node-request-run-*.bin",
		recordBytes: wayNodeRequestRecordBytes,
		read:        readWayNodeRequestRecord,
		write:       writeWayNodeRequestRecord,
		compare:     compareWayNodeRequests,
	})
}

func joinWayNodeRequests(ctx context.Context, sortedRequestsPath, globalNodesPath, outputPath string) (count int64, err error) {
	if ctx == nil {
		return 0, fmt.Errorf("way node join context is nil")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if sortedRequestsPath == "" || globalNodesPath == "" || outputPath == "" {
		return 0, fmt.Errorf("way node join paths are required")
	}

	requestsFile, err := openRegularBuilderInput(sortedRequestsPath, "sorted way node requests", wayNodeRequestRecordBytes)
	if err != nil {
		return 0, err
	}
	defer requestsFile.Close()
	nodesFile, err := openRegularBuilderInput(globalNodesPath, "global node index", globalNodeRecordBytes)
	if err != nil {
		return 0, err
	}
	defer nodesFile.Close()

	requests := bufio.NewReaderSize(requestsFile, joinBufferBytes)
	nodes := bufio.NewReaderSize(nodesFile, joinBufferBytes)
	var previousRequest wayNodeRequest
	hasPreviousRequest := false
	readRequest := func() (wayNodeRequest, bool, error) {
		request, ok, err := readWayNodeRequestRecord(requests)
		if err != nil || !ok {
			return request, ok, err
		}
		if hasPreviousRequest && compareWayNodeRequests(previousRequest, request) >= 0 {
			return wayNodeRequest{}, false, fmt.Errorf("sorted way node requests are not strictly ordered after node %d occurrence %d", previousRequest.NodeID, previousRequest.Occurrence)
		}
		previousRequest = request
		hasPreviousRequest = true
		return request, true, nil
	}

	var previousNode globalNodeRecord
	hasPreviousNode := false
	readNode := func() (globalNodeRecord, bool, error) {
		node, ok, err := readSequentialGlobalNodeRecord(nodes)
		if err != nil || !ok {
			return node, ok, err
		}
		if hasPreviousNode && node.ID <= previousNode.ID {
			return globalNodeRecord{}, false, fmt.Errorf("global node index IDs are not strictly sorted: %d follows %d", node.ID, previousNode.ID)
		}
		previousNode = node
		hasPreviousNode = true
		return node, true, nil
	}

	err = writeAtomicBuilderOutput(ctx, outputPath, ".resolved-way-nodes-*.tmp", func(writer io.Writer) error {
		request, hasRequest, err := readRequest()
		if err != nil {
			return err
		}
		node, hasNode, err := readNode()
		if err != nil {
			return err
		}
		var firstMissing *int64
		for hasRequest || hasNode {
			if err := ctx.Err(); err != nil {
				return err
			}
			switch {
			case !hasRequest:
				node, hasNode, err = readNode()
			case !hasNode:
				if firstMissing == nil {
					missing := request.NodeID
					firstMissing = &missing
				}
				request, hasRequest, err = readRequest()
			case node.ID < request.NodeID:
				node, hasNode, err = readNode()
			case node.ID > request.NodeID:
				if firstMissing == nil {
					missing := request.NodeID
					firstMissing = &missing
				}
				request, hasRequest, err = readRequest()
			default:
				if count == math.MaxInt64 {
					return fmt.Errorf("resolved way node count exceeds int64")
				}
				resolved := resolvedWayNode{
					Occurrence: request.Occurrence,
					Node: worldgraph.Node{
						ID: node.ID, Lon: node.Lon, Lat: node.Lat,
					},
				}
				if err := writeResolvedWayNodeRecord(writer, resolved); err != nil {
					return err
				}
				count++
				request, hasRequest, err = readRequest()
			}
			if err != nil {
				return err
			}
		}
		if firstMissing != nil {
			return fmt.Errorf("way node request references missing node %d", *firstMissing)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func sortResolvedWayNodes(ctx context.Context, input, output, runDir string, maxRecords int) error {
	expected := uint64(0)
	exhausted := false
	return sortFixedRecordFile(ctx, input, output, runDir, maxRecords, fixedRecordSortConfig[resolvedWayNode]{
		runPattern:  "resolved-way-node-run-*.bin",
		recordBytes: resolvedWayNodeRecordBytes,
		read:        readResolvedWayNodeRecord,
		write:       writeResolvedWayNodeRecord,
		compare:     compareResolvedWayNodes,
		validateOutput: func(record resolvedWayNode) error {
			if err := validateSequentialOccurrence(record.Occurrence, expected, exhausted); err != nil {
				return err
			}
			if expected == math.MaxUint64 {
				exhausted = true
			} else {
				expected++
			}
			return nil
		},
	})
}

func replayResolvedWayNodes(ctx context.Context, path string, consume func(resolvedWayNode) error) (err error) {
	if ctx == nil {
		return fmt.Errorf("resolved way node replay context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("resolved way node consumer is nil")
	}
	reader, err := openResolvedWayNodeReader(path)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, reader.Close())
	}()
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

type resolvedWayNodeReader struct {
	file                 *os.File
	reader               *bufio.Reader
	expectedOccurrence   uint64
	occurrenceSpaceEnded bool
	exhausted            bool
}

func openResolvedWayNodeReader(path string) (*resolvedWayNodeReader, error) {
	if path == "" {
		return nil, fmt.Errorf("resolved way node path is required")
	}
	file, err := openRegularBuilderInput(path, "resolved way nodes", resolvedWayNodeRecordBytes)
	if err != nil {
		return nil, err
	}
	return &resolvedWayNodeReader{file: file, reader: bufio.NewReaderSize(file, joinBufferBytes)}, nil
}

func (reader *resolvedWayNodeReader) Next() (resolvedWayNode, bool, error) {
	if reader == nil {
		return resolvedWayNode{}, false, fmt.Errorf("resolved way node reader is nil")
	}
	if reader.file == nil {
		return resolvedWayNode{}, false, fmt.Errorf("resolved way node reader is closed")
	}
	if reader.exhausted {
		return resolvedWayNode{}, false, nil
	}
	record, ok, err := readResolvedWayNodeRecord(reader.reader)
	if err != nil {
		return resolvedWayNode{}, false, err
	}
	if !ok {
		reader.exhausted = true
		return resolvedWayNode{}, false, nil
	}
	if err := validateSequentialOccurrence(record.Occurrence, reader.expectedOccurrence, reader.occurrenceSpaceEnded); err != nil {
		return resolvedWayNode{}, false, err
	}
	if reader.expectedOccurrence == math.MaxUint64 {
		reader.occurrenceSpaceEnded = true
	} else {
		reader.expectedOccurrence++
	}
	return record, true, nil
}

func (reader *resolvedWayNodeReader) Close() error {
	if reader == nil || reader.file == nil {
		return nil
	}
	err := reader.file.Close()
	reader.file = nil
	reader.reader = nil
	return err
}

func writeWayNodeRequestRecord(writer io.Writer, request wayNodeRequest) error {
	if writer == nil {
		return fmt.Errorf("way node request writer is nil")
	}
	var encoded [wayNodeRequestRecordBytes]byte
	copy(encoded[0:8], nodeKey(request.NodeID))
	binary.BigEndian.PutUint64(encoded[8:16], request.Occurrence)
	return writeFixedRecord(writer, encoded[:])
}

func readWayNodeRequestRecord(reader io.Reader) (wayNodeRequest, bool, error) {
	if reader == nil {
		return wayNodeRequest{}, false, fmt.Errorf("way node request reader is nil")
	}
	var encoded [wayNodeRequestRecordBytes]byte
	count, err := io.ReadFull(reader, encoded[:])
	if errors.Is(err, io.EOF) && count == 0 {
		return wayNodeRequest{}, false, nil
	}
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return wayNodeRequest{}, false, fmt.Errorf("%w: way node request", ErrTruncatedFixedRecord)
		}
		return wayNodeRequest{}, false, err
	}
	return wayNodeRequest{
		NodeID:     decodeNodeKey(encoded[0:8]),
		Occurrence: binary.BigEndian.Uint64(encoded[8:16]),
	}, true, nil
}

func writeResolvedWayNodeRecord(writer io.Writer, record resolvedWayNode) error {
	if writer == nil {
		return fmt.Errorf("resolved way node writer is nil")
	}
	encoded, err := encodeResolvedWayNodeRecord(record)
	if err != nil {
		return err
	}
	return writeFixedRecord(writer, encoded[:])
}

func encodeResolvedWayNodeRecord(record resolvedWayNode) ([resolvedWayNodeRecordBytes]byte, error) {
	var encoded [resolvedWayNodeRecordBytes]byte
	if !validOSMPosition(record.Node.Lon, record.Node.Lat) {
		return encoded, fmt.Errorf("%w: resolved node %d has invalid coordinates", worldgraph.ErrInvalidChunk, record.Node.ID)
	}
	binary.BigEndian.PutUint64(encoded[0:8], record.Occurrence)
	copy(encoded[8:16], nodeKey(record.Node.ID))
	binary.BigEndian.PutUint64(encoded[16:24], math.Float64bits(record.Node.Lon))
	binary.BigEndian.PutUint64(encoded[24:32], math.Float64bits(record.Node.Lat))
	return encoded, nil
}

func readResolvedWayNodeRecord(reader io.Reader) (resolvedWayNode, bool, error) {
	if reader == nil {
		return resolvedWayNode{}, false, fmt.Errorf("resolved way node reader is nil")
	}
	var encoded [resolvedWayNodeRecordBytes]byte
	count, err := io.ReadFull(reader, encoded[:])
	if errors.Is(err, io.EOF) && count == 0 {
		return resolvedWayNode{}, false, nil
	}
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return resolvedWayNode{}, false, fmt.Errorf("%w: resolved way node", ErrTruncatedFixedRecord)
		}
		return resolvedWayNode{}, false, err
	}
	record := resolvedWayNode{
		Occurrence: binary.BigEndian.Uint64(encoded[0:8]),
		Node: worldgraph.Node{
			ID:  decodeNodeKey(encoded[8:16]),
			Lon: math.Float64frombits(binary.BigEndian.Uint64(encoded[16:24])),
			Lat: math.Float64frombits(binary.BigEndian.Uint64(encoded[24:32])),
		},
	}
	if !validOSMPosition(record.Node.Lon, record.Node.Lat) {
		return resolvedWayNode{}, false, fmt.Errorf("%w: resolved node %d has invalid coordinates", worldgraph.ErrInvalidChunk, record.Node.ID)
	}
	return record, true, nil
}

func readSequentialGlobalNodeRecord(reader io.Reader) (globalNodeRecord, bool, error) {
	if reader == nil {
		return globalNodeRecord{}, false, fmt.Errorf("global node reader is nil")
	}
	var encoded [globalNodeRecordBytes]byte
	count, err := io.ReadFull(reader, encoded[:])
	if errors.Is(err, io.EOF) && count == 0 {
		return globalNodeRecord{}, false, nil
	}
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return globalNodeRecord{}, false, fmt.Errorf("%w: global node record", ErrTruncatedFixedRecord)
		}
		return globalNodeRecord{}, false, err
	}
	record, err := decodeGlobalNodeRecord(encoded[:])
	if err != nil {
		return globalNodeRecord{}, false, err
	}
	return record, true, nil
}

func compareWayNodeRequests(left, right wayNodeRequest) int {
	if left.NodeID < right.NodeID {
		return -1
	}
	if left.NodeID > right.NodeID {
		return 1
	}
	if left.Occurrence < right.Occurrence {
		return -1
	}
	if left.Occurrence > right.Occurrence {
		return 1
	}
	return 0
}

func compareResolvedWayNodes(left, right resolvedWayNode) int {
	if left.Occurrence < right.Occurrence {
		return -1
	}
	if left.Occurrence > right.Occurrence {
		return 1
	}
	// Duplicate occurrences are invalid, but a total tie-break makes failures
	// deterministic regardless of run size and merge grouping.
	if left.Node.ID < right.Node.ID {
		return -1
	}
	if left.Node.ID > right.Node.ID {
		return 1
	}
	leftLon, rightLon := math.Float64bits(left.Node.Lon), math.Float64bits(right.Node.Lon)
	if leftLon < rightLon {
		return -1
	}
	if leftLon > rightLon {
		return 1
	}
	leftLat, rightLat := math.Float64bits(left.Node.Lat), math.Float64bits(right.Node.Lat)
	if leftLat < rightLat {
		return -1
	}
	if leftLat > rightLat {
		return 1
	}
	return 0
}

func validateSequentialOccurrence(got, expected uint64, exhausted bool) error {
	if exhausted || got < expected {
		return fmt.Errorf("resolved way node occurrence %d is duplicate or decreasing; expected %d", got, expected)
	}
	if got > expected {
		return fmt.Errorf("resolved way node occurrence gap: got %d, expected %d", got, expected)
	}
	return nil
}

func writeAtomicBuilderOutput(ctx context.Context, outputPath, pattern string, write func(io.Writer) error) error {
	if ctx == nil {
		return fmt.Errorf("atomic output context is nil")
	}
	if outputPath == "" || pattern == "" {
		return fmt.Errorf("atomic output path and pattern are required")
	}
	if write == nil {
		return fmt.Errorf("atomic output writer is nil")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(outputPath), pattern)
	if err != nil {
		return err
	}
	temporary := file.Name()
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		_ = os.Remove(temporary)
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	buffered := bufio.NewWriterSize(file, joinBufferBytes)
	if err := write(buffered); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporary, outputPath); err != nil {
		return err
	}
	return syncBuilderDirectory(filepath.Dir(outputPath))
}

func openRegularBuilderInput(path, description string, recordBytes int64) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() {
		return nil, errors.Join(fmt.Errorf("%s is not a regular file", description), file.Close())
	}
	if recordBytes > 0 && info.Size()%recordBytes != 0 {
		return nil, errors.Join(fmt.Errorf("%w: %s size %d", ErrTruncatedFixedRecord, description, info.Size()), file.Close())
	}
	return file, nil
}

type fixedRecordSortConfig[T any] struct {
	runPattern     string
	recordBytes    int64
	read           func(io.Reader) (T, bool, error)
	write          func(io.Writer, T) error
	compare        func(T, T) int
	validateOutput func(T) error
}

func sortFixedRecordFile[T any](ctx context.Context, input, output, runDir string, maxRecords int, config fixedRecordSortConfig[T]) error {
	if ctx == nil {
		return fmt.Errorf("external fixed-record sort context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if input == "" || output == "" || runDir == "" {
		return fmt.Errorf("external fixed-record sort paths are required")
	}
	if maxRecords < 1 {
		return fmt.Errorf("external fixed-record sort record limit must be positive")
	}
	if config.runPattern == "" || config.recordBytes < 1 || config.read == nil || config.write == nil || config.compare == nil {
		return fmt.Errorf("external fixed-record sort configuration is invalid")
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return err
	}

	source, err := openRegularBuilderInput(input, "external sort input", config.recordBytes)
	if err != nil {
		return err
	}
	defer source.Close()

	var temporaryPaths []string
	defer func() {
		for _, path := range temporaryPaths {
			_ = os.Remove(path)
		}
	}()
	newTemporaryPath := func(directory, pattern string) (string, error) {
		file, err := os.CreateTemp(directory, pattern)
		if err != nil {
			return "", err
		}
		path := file.Name()
		closeErr := file.Close()
		removeErr := os.Remove(path)
		if closeErr != nil || removeErr != nil {
			_ = os.Remove(path)
			return "", errors.Join(closeErr, removeErr)
		}
		temporaryPaths = append(temporaryPaths, path)
		return path, nil
	}

	reader := bufio.NewReaderSize(source, joinBufferBytes)
	batch := make([]T, 0, min(maxRecords, 4_096))
	var runs []string
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		sort.Slice(batch, func(i, j int) bool { return config.compare(batch[i], batch[j]) < 0 })
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := newTemporaryPath(runDir, config.runPattern)
		if err != nil {
			return err
		}
		if err := writeFixedRecordRun(ctx, path, batch, config); err != nil {
			return err
		}
		runs = append(runs, path)
		batch = batch[:0]
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		record, ok, err := config.read(reader)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		batch = append(batch, record)
		if len(batch) == maxRecords {
			if err := flushBatch(); err != nil {
				return err
			}
		}
	}
	if err := flushBatch(); err != nil {
		return err
	}

	for len(runs) > maxMergeFanIn {
		nextRuns := make([]string, 0, (len(runs)+maxMergeFanIn-1)/maxMergeFanIn)
		for start := 0; start < len(runs); start += maxMergeFanIn {
			if err := ctx.Err(); err != nil {
				return err
			}
			end := min(start+maxMergeFanIn, len(runs))
			path, err := newTemporaryPath(runDir, config.runPattern)
			if err != nil {
				return err
			}
			if err := mergeFixedRecordRuns(ctx, runs[start:end], path, config, nil); err != nil {
				return err
			}
			nextRuns = append(nextRuns, path)
			for _, consumed := range runs[start:end] {
				_ = os.Remove(consumed)
			}
		}
		runs = nextRuns
	}

	temporaryOutput, err := newTemporaryPath(filepath.Dir(output), ".fixed-sort-output-*.tmp")
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		file, err := os.OpenFile(temporaryOutput, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	} else if err := mergeFixedRecordRuns(ctx, runs, temporaryOutput, config, config.validateOutput); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporaryOutput, output); err != nil {
		return err
	}
	return syncBuilderDirectory(filepath.Dir(output))
}

func writeFixedRecordRun[T any](ctx context.Context, path string, records []T, config fixedRecordSortConfig[T]) error {
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
	writer := bufio.NewWriterSize(file, joinBufferBytes)
	for index, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if index > 0 && config.compare(records[index-1], record) > 0 {
			return fmt.Errorf("external sort run is out of order")
		}
		if err := config.write(writer, record); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
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

type fixedMergeItem[T any] struct {
	record T
	reader int
}

type fixedMergeHeap[T any] struct {
	items   []fixedMergeItem[T]
	compare func(T, T) int
}

func (items fixedMergeHeap[T]) Len() int { return len(items.items) }
func (items fixedMergeHeap[T]) Less(i, j int) bool {
	comparison := items.compare(items.items[i].record, items.items[j].record)
	if comparison != 0 {
		return comparison < 0
	}
	return items.items[i].reader < items.items[j].reader
}
func (items fixedMergeHeap[T]) Swap(i, j int) {
	items.items[i], items.items[j] = items.items[j], items.items[i]
}
func (items *fixedMergeHeap[T]) Push(value any) {
	items.items = append(items.items, value.(fixedMergeItem[T]))
}
func (items *fixedMergeHeap[T]) Pop() any {
	old := items.items
	last := old[len(old)-1]
	items.items = old[:len(old)-1]
	return last
}

func mergeFixedRecordRuns[T any](
	ctx context.Context,
	paths []string,
	output string,
	config fixedRecordSortConfig[T],
	validateOutput func(T) error,
) error {
	if len(paths) < 1 || len(paths) > maxMergeFanIn {
		return fmt.Errorf("external sort merge fan-in %d is outside 1..%d", len(paths), maxMergeFanIn)
	}
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

	files := make([]*os.File, len(paths))
	readers := make([]*bufio.Reader, len(paths))
	defer func() {
		for _, input := range files {
			if input != nil {
				_ = input.Close()
			}
		}
	}()
	items := &fixedMergeHeap[T]{compare: config.compare}
	for index, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		input, err := openRegularBuilderInput(path, "external sort run", config.recordBytes)
		if err != nil {
			return err
		}
		files[index] = input
		readers[index] = bufio.NewReaderSize(input, joinBufferBytes)
		record, ok, err := config.read(readers[index])
		if err != nil {
			return err
		}
		if ok {
			heap.Push(items, fixedMergeItem[T]{record: record, reader: index})
		}
	}

	writer := bufio.NewWriterSize(file, joinBufferBytes)
	var previousOutput T
	hasPreviousOutput := false
	for items.Len() > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := heap.Pop(items).(fixedMergeItem[T])
		if hasPreviousOutput && config.compare(previousOutput, item.record) > 0 {
			return fmt.Errorf("external sort merge output is out of order")
		}
		if validateOutput != nil {
			if err := validateOutput(item.record); err != nil {
				return err
			}
		}
		if err := config.write(writer, item.record); err != nil {
			return err
		}
		previousOutput = item.record
		hasPreviousOutput = true

		next, ok, err := config.read(readers[item.reader])
		if err != nil {
			return err
		}
		if ok {
			if config.compare(item.record, next) > 0 {
				return fmt.Errorf("external sort run is out of order")
			}
			heap.Push(items, fixedMergeItem[T]{record: next, reader: item.reader})
		}
	}
	if err := writer.Flush(); err != nil {
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
