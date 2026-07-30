package builder

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const (
	int64RecordBytes = 8
	maxMergeFanIn    = 64
)

var ErrTruncatedFixedRecord = errors.New("truncated fixed-width record")

func sortUniqueInt64File(ctx context.Context, input, output, runDir string, maxRecords int) error {
	if ctx == nil {
		return fmt.Errorf("external sort context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if input == "" || output == "" || runDir == "" {
		return fmt.Errorf("external sort paths are required")
	}
	if maxRecords < 1 {
		return fmt.Errorf("external sort record limit must be positive")
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
		return fmt.Errorf("external sort input is not a regular file")
	}

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
	batch := make([]int64, 0, maxRecords)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		path, err := newRunPath("run-*.refs")
		if err != nil {
			return err
		}
		if err := writeSortedInt64Run(ctx, path, batch); err != nil {
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
		value, ok, err := readInt64Record(source)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		batch = append(batch, value)
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
			path, err := newRunPath("merge-*.refs")
			if err != nil {
				return err
			}
			if err := mergeInt64Runs(ctx, runs[start:end], path); err != nil {
				return err
			}
			next = append(next, path)
			for _, consumed := range runs[start:end] {
				_ = os.Remove(consumed)
			}
		}
		runs = next
	}

	outputFile, err := os.CreateTemp(filepath.Dir(output), ".sorted-*.tmp")
	if err != nil {
		return err
	}
	temporaryOutput := outputFile.Name()
	closed := false
	defer func() {
		if !closed {
			_ = outputFile.Close()
		}
		_ = os.Remove(temporaryOutput)
	}()
	if err := outputFile.Chmod(0o600); err != nil {
		return err
	}
	if err := outputFile.Close(); err != nil {
		return err
	}
	closed = true
	if len(runs) == 0 {
		if err := syncRegularBuilderFile(temporaryOutput); err != nil {
			return err
		}
	} else {
		if err := os.Remove(temporaryOutput); err != nil {
			return err
		}
		if err := mergeInt64Runs(ctx, runs, temporaryOutput); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporaryOutput, output); err != nil {
		return err
	}
	if err := syncBuilderDirectory(filepath.Dir(output)); err != nil {
		return err
	}
	return nil
}

func writeSortedInt64Run(ctx context.Context, path string, values []int64) error {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
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
	var previous int64
	hasPrevious := false
	for _, value := range values {
		if err := ctx.Err(); err != nil {
			return err
		}
		if hasPrevious && value == previous {
			continue
		}
		if err := writeInt64Record(file, value); err != nil {
			return err
		}
		previous = value
		hasPrevious = true
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

func mergeInt64Runs(ctx context.Context, paths []string, output string) error {
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
	defer func() {
		for _, reader := range readers {
			if reader != nil {
				_ = reader.Close()
			}
		}
	}()
	items := int64MergeHeap{}
	for index, path := range paths {
		reader, err := os.Open(path)
		if err != nil {
			return err
		}
		readers[index] = reader
		value, ok, err := readInt64Record(reader)
		if err != nil {
			return err
		}
		if ok {
			heap.Push(&items, int64MergeItem{value: value, reader: index})
		}
	}
	var previous int64
	hasPrevious := false
	for items.Len() > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := heap.Pop(&items).(int64MergeItem)
		if !hasPrevious || item.value != previous {
			if err := writeInt64Record(file, item.value); err != nil {
				return err
			}
			previous = item.value
			hasPrevious = true
		}
		next, ok, err := readInt64Record(readers[item.reader])
		if err != nil {
			return err
		}
		if ok {
			heap.Push(&items, int64MergeItem{value: next, reader: item.reader})
		}
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

func writeInt64Record(writer io.Writer, value int64) error {
	return writeFixedRecord(writer, nodeKey(value))
}

func readInt64Record(reader io.Reader) (int64, bool, error) {
	var encoded [int64RecordBytes]byte
	count, err := io.ReadFull(reader, encoded[:])
	if errors.Is(err, io.EOF) && count == 0 {
		return 0, false, nil
	}
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, false, fmt.Errorf("%w: int64 record", ErrTruncatedFixedRecord)
		}
		return 0, false, err
	}
	return decodeNodeKey(encoded[:]), true, nil
}

func writeFixedRecord(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func syncRegularBuilderFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func syncBuilderDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	return directory.Close()
}

type int64MergeItem struct {
	value  int64
	reader int
}

type int64MergeHeap []int64MergeItem

func (items int64MergeHeap) Len() int { return len(items) }
func (items int64MergeHeap) Less(i, j int) bool {
	if items[i].value != items[j].value {
		return items[i].value < items[j].value
	}
	return items[i].reader < items[j].reader
}
func (items int64MergeHeap) Swap(i, j int) { items[i], items[j] = items[j], items[i] }
func (items *int64MergeHeap) Push(value any) {
	*items = append(*items, value.(int64MergeItem))
}
func (items *int64MergeHeap) Pop() any {
	old := *items
	last := old[len(old)-1]
	*items = old[:len(old)-1]
	return last
}
