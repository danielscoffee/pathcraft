package builder

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestExternalSortDeduplicatesSignedRecords(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input.refs")
	values := []int64{5, -1, 5, math.MinInt64, 0, math.MaxInt64, -1}
	writeInt64TestFile(t, input, values)
	first := filepath.Join(root, "first.refs")
	firstRuns := filepath.Join(root, "runs-first")
	if err := sortUniqueInt64File(context.Background(), input, first, firstRuns, 3); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(root, "second.refs")
	if err := sortUniqueInt64File(context.Background(), input, second, filepath.Join(root, "runs-second"), 3); err != nil {
		t.Fatal(err)
	}
	want := []int64{math.MinInt64, -1, 0, 5, math.MaxInt64}
	if got := readInt64TestFile(t, first); !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted values = %v, want %v", got, want)
	}
	firstBytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("external sort output is not deterministic")
	}
	info, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("output mode = %o, want 600", info.Mode().Perm())
	}
	assertDirectoryEmpty(t, firstRuns)
}

func TestExternalSortMergesMoreThanFanIn(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input.refs")
	values := make([]int64, 200)
	for index := range values {
		values[index] = int64(len(values) - index - 1)
	}
	writeInt64TestFile(t, input, values)
	output := filepath.Join(root, "output.refs")
	if err := sortUniqueInt64File(context.Background(), input, output, filepath.Join(root, "runs"), 1); err != nil {
		t.Fatal(err)
	}
	want := append([]int64(nil), values...)
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if got := readInt64TestFile(t, output); !reflect.DeepEqual(got, want) {
		t.Fatalf("merged values = %v, want %v", got, want)
	}
}

func TestExternalSortRejectsTruncatedRecord(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input.refs")
	if err := os.WriteFile(input, []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sortUniqueInt64File(context.Background(), input, filepath.Join(root, "output.refs"), filepath.Join(root, "runs"), 3); !errors.Is(err, ErrTruncatedFixedRecord) {
		t.Fatalf("sortUniqueInt64File() error = %v, want ErrTruncatedFixedRecord", err)
	}
}

func TestExternalSortCancellationCleansRuns(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input.refs")
	writeInt64TestFile(t, input, []int64{3, 2, 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runDir := filepath.Join(root, "runs")
	output := filepath.Join(root, "output.refs")
	if err := sortUniqueInt64File(ctx, input, output, runDir, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("sortUniqueInt64File() error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled output exists: %v", err)
	}
	assertDirectoryEmpty(t, runDir)
}

func writeInt64TestFile(t *testing.T, path string, values []int64) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if err := writeInt64Record(file, value); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func readInt64TestFile(t *testing.T, path string) []int64 {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var values []int64
	for {
		value, ok, err := readInt64Record(file)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return values
		}
		values = append(values, value)
	}
}

func assertDirectoryEmpty(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory %q contains %v", path, entries)
	}
}
