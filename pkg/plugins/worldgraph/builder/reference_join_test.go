package builder

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

func TestSpoolWayNodeRequestsAssignsStableOccurrences(t *testing.T) {
	root := t.TempDir()
	waysPath := filepath.Join(root, "ways.spool")
	writeWaySpoolTestFile(t, waysPath, []Way{
		{ID: -10, NodeIDs: []int64{-2, 5, -2}, Tags: map[string]string{"highway": "road"}},
		{ID: 20, NodeIDs: []int64{math.MaxInt64, math.MinInt64}, Tags: map[string]string{"highway": "service"}},
	}, 3)

	first := filepath.Join(root, "first.requests")
	count, err := spoolWayNodeRequests(context.Background(), waysPath, first, 3)
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("spoolWayNodeRequests() count = %d, want 5", count)
	}
	want := []wayNodeRequest{
		{NodeID: -2, Occurrence: 0},
		{NodeID: 5, Occurrence: 1},
		{NodeID: -2, Occurrence: 2},
		{NodeID: math.MaxInt64, Occurrence: 3},
		{NodeID: math.MinInt64, Occurrence: 4},
	}
	if got := readWayNodeRequestTestFile(t, first); !reflect.DeepEqual(got, want) {
		t.Fatalf("spooled requests = %+v, want %+v", got, want)
	}
	assertBuilderFileModeAndSize(t, first, 0o600, int64(len(want)*wayNodeRequestRecordBytes))

	second := filepath.Join(root, "second.requests")
	if _, err := spoolWayNodeRequests(context.Background(), waysPath, second, 3); err != nil {
		t.Fatal(err)
	}
	assertFilesEqual(t, first, second)
}

func TestSpoolWayNodeRequestsIsAtomicOnCorruptInput(t *testing.T) {
	root := t.TempDir()
	waysPath := filepath.Join(root, "ways.spool")
	writeWaySpoolTestFile(t, waysPath, []Way{
		{ID: 1, NodeIDs: []int64{1, 2}, Tags: map[string]string{"highway": "road"}},
	}, DefaultMaxWayNodes)
	file, err := os.OpenFile(waysPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte{0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "requests.raw")
	if err := os.WriteFile(output, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	count, err := spoolWayNodeRequests(context.Background(), waysPath, output, DefaultMaxWayNodes)
	if !errors.Is(err, ErrTruncatedFixedRecord) {
		t.Fatalf("spoolWayNodeRequests() error = %v, want ErrTruncatedFixedRecord", err)
	}
	if count != 0 {
		t.Fatalf("failed spool count = %d, want 0", count)
	}
	if got, err := os.ReadFile(output); err != nil || string(got) != "previous" {
		t.Fatalf("atomic output = %q, %v", got, err)
	}
}

func TestSortWayNodeRequestsHandlesRepeatedSignedIDsDeterministically(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "requests.raw")
	records := []wayNodeRequest{
		{NodeID: 5, Occurrence: 4},
		{NodeID: -1, Occurrence: 3},
		{NodeID: math.MinInt64, Occurrence: 6},
		{NodeID: 5, Occurrence: 1},
		{NodeID: math.MaxInt64, Occurrence: 0},
		{NodeID: -1, Occurrence: 2},
		{NodeID: 0, Occurrence: 5},
	}
	writeWayNodeRequestTestFile(t, input, records)

	first := filepath.Join(root, "sorted-one.bin")
	firstRuns := filepath.Join(root, "runs-one")
	if err := sortWayNodeRequests(context.Background(), input, first, firstRuns, 1); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(root, "sorted-four.bin")
	secondRuns := filepath.Join(root, "runs-four")
	if err := sortWayNodeRequests(context.Background(), input, second, secondRuns, 4); err != nil {
		t.Fatal(err)
	}
	want := append([]wayNodeRequest(nil), records...)
	sort.Slice(want, func(i, j int) bool { return compareWayNodeRequests(want[i], want[j]) < 0 })
	if got := readWayNodeRequestTestFile(t, first); !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted requests = %+v, want %+v", got, want)
	}
	assertFilesEqual(t, first, second)
	assertBuilderFileModeAndSize(t, first, 0o600, int64(len(records)*wayNodeRequestRecordBytes))
	assertDirectoryEmpty(t, firstRuns)
	assertDirectoryEmpty(t, secondRuns)
}

func TestSortWayNodeRequestsMergesMoreThan64TinyRunsAndPreservesRecords(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "requests.raw")
	records := make([]wayNodeRequest, 0, 131)
	for occurrence := 0; occurrence < 130; occurrence++ {
		records = append(records, wayNodeRequest{
			NodeID:     int64(occurrence%17) - 8,
			Occurrence: uint64(129 - occurrence),
		})
	}
	// Exact duplicate records remain records; this sort never deduplicates.
	records = append(records, records[17])
	writeWayNodeRequestTestFile(t, input, records)
	output := filepath.Join(root, "requests.sorted")
	runDir := filepath.Join(root, "runs")
	if err := sortWayNodeRequests(context.Background(), input, output, runDir, 1); err != nil {
		t.Fatal(err)
	}
	want := append([]wayNodeRequest(nil), records...)
	sort.Slice(want, func(i, j int) bool { return compareWayNodeRequests(want[i], want[j]) < 0 })
	if got := readWayNodeRequestTestFile(t, output); !reflect.DeepEqual(got, want) {
		t.Fatalf("merged %d records incorrectly", len(records))
	}
	assertDirectoryEmpty(t, runDir)
}

func TestSortWayNodeRequestsRejectsTruncationAndCleansCancellation(t *testing.T) {
	t.Run("truncation", func(t *testing.T) {
		root := t.TempDir()
		input := filepath.Join(root, "requests.raw")
		if err := os.WriteFile(input, make([]byte, wayNodeRequestRecordBytes-1), 0o600); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(root, "requests.sorted")
		runDir := filepath.Join(root, "runs")
		err := sortWayNodeRequests(context.Background(), input, output, runDir, 2)
		if !errors.Is(err, ErrTruncatedFixedRecord) {
			t.Fatalf("sortWayNodeRequests() error = %v, want ErrTruncatedFixedRecord", err)
		}
		assertPathMissing(t, output)
		assertDirectoryEmpty(t, runDir)
	})

	t.Run("cancellation after runs", func(t *testing.T) {
		root := t.TempDir()
		input := filepath.Join(root, "requests.raw")
		records := make([]wayNodeRequest, 300)
		for index := range records {
			records[index] = wayNodeRequest{NodeID: int64(300 - index), Occurrence: uint64(index)}
		}
		writeWayNodeRequestTestFile(t, input, records)
		output := filepath.Join(root, "requests.sorted")
		runDir := filepath.Join(root, "runs")
		ctx := &cancelAfterChecksContext{Context: context.Background(), remaining: 80}
		err := sortWayNodeRequests(ctx, input, output, runDir, 1)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("sortWayNodeRequests() error = %v, want context.Canceled", err)
		}
		assertPathMissing(t, output)
		assertDirectoryEmpty(t, runDir)
	})
}

func TestJoinWayNodeRequestsSequentiallyResolvesRepeatsSignedIDsAndPoles(t *testing.T) {
	root := t.TempDir()
	requestsPath := filepath.Join(root, "requests.sorted")
	requests := []wayNodeRequest{
		{NodeID: -5, Occurrence: 1},
		{NodeID: -5, Occurrence: 4},
		{NodeID: 0, Occurrence: 3},
		{NodeID: 7, Occurrence: 0},
		{NodeID: 9, Occurrence: 2},
	}
	writeWayNodeRequestTestFile(t, requestsPath, requests)
	nodesPath := filepath.Join(root, "nodes.idx")
	writeGlobalNodeTestFile(t, nodesPath, []globalNodeRecord{
		globalNodeTestRecord(t, -10, -100, -40),
		globalNodeTestRecord(t, -5, -70, -10),
		globalNodeTestRecord(t, 0, 0, 0),
		globalNodeTestRecord(t, 7, 12, 55),
		{ID: 9, Lon: 180, Lat: 90},
		globalNodeTestRecord(t, 100, 139, 35),
	})

	joinedPath := filepath.Join(root, "resolved.by-node")
	count, err := joinWayNodeRequests(context.Background(), requestsPath, nodesPath, joinedPath)
	if err != nil {
		t.Fatal(err)
	}
	if count != int64(len(requests)) {
		t.Fatalf("join count = %d, want %d", count, len(requests))
	}
	joined := readResolvedWayNodeRawTestFile(t, joinedPath)
	if got, want := resolvedOccurrences(joined), []uint64{1, 4, 3, 0, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("joined occurrences = %v, want %v", got, want)
	}
	if joined[0].Node.ID != -5 || joined[1].Node.ID != -5 || joined[4].Node.Lat != 90 || joined[4].Node.Lon != 180 {
		t.Fatalf("joined nodes = %+v", joined)
	}
	for _, record := range joined {
		if record.Node.Owner != (worldgraph.TileID{}) {
			t.Fatalf("resolved owner was encoded: %+v", record.Node.Owner)
		}
	}
	assertBuilderFileModeAndSize(t, joinedPath, 0o600, int64(len(requests)*resolvedWayNodeRecordBytes))

	orderedPath := filepath.Join(root, "resolved.by-occurrence")
	if err := sortResolvedWayNodes(context.Background(), joinedPath, orderedPath, filepath.Join(root, "runs"), 2); err != nil {
		t.Fatal(err)
	}
	var replayed []resolvedWayNode
	if err := replayResolvedWayNodes(context.Background(), orderedPath, func(record resolvedWayNode) error {
		replayed = append(replayed, record)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, want := resolvedNodeIDs(replayed), []int64{7, -5, 9, 0, -5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replayed node IDs = %v, want %v", got, want)
	}
}

func TestJoinWayNodeRequestsRejectsMissingReferencesAtomically(t *testing.T) {
	root := t.TempDir()
	requestsPath := filepath.Join(root, "requests.sorted")
	writeWayNodeRequestTestFile(t, requestsPath, []wayNodeRequest{
		{NodeID: 1, Occurrence: 0},
		{NodeID: 2, Occurrence: 1},
		{NodeID: 3, Occurrence: 2},
	})
	nodesPath := filepath.Join(root, "nodes.idx")
	writeGlobalNodeTestFile(t, nodesPath, []globalNodeRecord{
		globalNodeTestRecord(t, 1, 0, 0),
		globalNodeTestRecord(t, 3, 1, 1),
	})
	output := filepath.Join(root, "resolved.bin")
	if err := os.WriteFile(output, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	count, err := joinWayNodeRequests(context.Background(), requestsPath, nodesPath, output)
	if err == nil || !strings.Contains(err.Error(), "missing node 2") {
		t.Fatalf("joinWayNodeRequests() error = %v, want missing node 2", err)
	}
	if count != 0 {
		t.Fatalf("failed join count = %d, want 0", count)
	}
	if got, err := os.ReadFile(output); err != nil || string(got) != "previous" {
		t.Fatalf("atomic output = %q, %v", got, err)
	}
}

func TestJoinWayNodeRequestsValidatesBothCompleteStreams(t *testing.T) {
	tests := []struct {
		name          string
		writeRequests func(*testing.T, string)
		writeNodes    func(*testing.T, string)
		wantError     error
		wantText      string
	}{
		{
			name: "decreasing nodes after final request",
			writeRequests: func(t *testing.T, path string) {
				writeWayNodeRequestTestFile(t, path, []wayNodeRequest{{NodeID: 2, Occurrence: 0}})
			},
			writeNodes: func(t *testing.T, path string) {
				writeGlobalNodeTestFile(t, path, []globalNodeRecord{
					globalNodeTestRecord(t, 2, 0, 0),
					globalNodeTestRecord(t, 1, 1, 1),
				})
			},
			wantText: "not strictly sorted",
		},
		{
			name: "duplicate nodes",
			writeRequests: func(t *testing.T, path string) {
				writeWayNodeRequestTestFile(t, path, nil)
			},
			writeNodes: func(t *testing.T, path string) {
				record := globalNodeTestRecord(t, 1, 0, 0)
				writeGlobalNodeTestFile(t, path, []globalNodeRecord{record, record})
			},
			wantText: "not strictly sorted",
		},
		{
			name: "corrupt trailing node",
			writeRequests: func(t *testing.T, path string) {
				writeWayNodeRequestTestFile(t, path, nil)
			},
			writeNodes: func(t *testing.T, path string) {
				writeGlobalNodeTestFile(t, path, []globalNodeRecord{globalNodeTestRecord(t, 1, 0, 0)})
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data[39] = 1
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: worldgraph.ErrInvalidChunk,
		},
		{
			name: "truncated nodes",
			writeRequests: func(t *testing.T, path string) {
				writeWayNodeRequestTestFile(t, path, nil)
			},
			writeNodes: func(t *testing.T, path string) {
				if err := os.WriteFile(path, make([]byte, globalNodeRecordBytes-1), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: ErrTruncatedFixedRecord,
		},
		{
			name: "decreasing requests",
			writeRequests: func(t *testing.T, path string) {
				writeWayNodeRequestTestFile(t, path, []wayNodeRequest{
					{NodeID: 2, Occurrence: 0}, {NodeID: 1, Occurrence: 1},
				})
			},
			writeNodes: func(t *testing.T, path string) {
				writeGlobalNodeTestFile(t, path, []globalNodeRecord{
					globalNodeTestRecord(t, 1, 0, 0), globalNodeTestRecord(t, 2, 1, 1),
				})
			},
			wantText: "requests are not strictly ordered",
		},
		{
			name: "truncated requests",
			writeRequests: func(t *testing.T, path string) {
				if err := os.WriteFile(path, make([]byte, wayNodeRequestRecordBytes-1), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			writeNodes: func(t *testing.T, path string) {
				writeGlobalNodeTestFile(t, path, nil)
			},
			wantError: ErrTruncatedFixedRecord,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			requests := filepath.Join(root, "requests.sorted")
			nodes := filepath.Join(root, "nodes.idx")
			output := filepath.Join(root, "resolved.bin")
			test.writeRequests(t, requests)
			test.writeNodes(t, nodes)
			_, err := joinWayNodeRequests(context.Background(), requests, nodes, output)
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("joinWayNodeRequests() error = %v, want %v", err, test.wantError)
			}
			if test.wantText != "" && (err == nil || !strings.Contains(err.Error(), test.wantText)) {
				t.Fatalf("joinWayNodeRequests() error = %v, want text %q", err, test.wantText)
			}
			if test.wantError == nil && test.wantText == "" && err == nil {
				t.Fatal("joinWayNodeRequests() error = nil")
			}
			assertPathMissing(t, output)
		})
	}
}

func TestSortResolvedWayNodesIsDeterministicAcrossRunSizesAndFanIn(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "resolved.by-node")
	records := make([]resolvedWayNode, 130)
	for index := range records {
		occurrence := uint64(129 - index)
		latitude := float64(int(occurrence)%181 - 90)
		records[index] = resolvedWayNode{
			Occurrence: occurrence,
			Node: worldgraph.Node{
				ID: int64(occurrence%19) - 9, Lon: float64(int(occurrence)%361 - 180), Lat: latitude,
			},
		}
	}
	writeResolvedWayNodeTestFile(t, input, records)
	one := filepath.Join(root, "one.bin")
	oneRuns := filepath.Join(root, "one-runs")
	if err := sortResolvedWayNodes(context.Background(), input, one, oneRuns, 1); err != nil {
		t.Fatal(err)
	}
	seventeen := filepath.Join(root, "seventeen.bin")
	seventeenRuns := filepath.Join(root, "seventeen-runs")
	if err := sortResolvedWayNodes(context.Background(), input, seventeen, seventeenRuns, 17); err != nil {
		t.Fatal(err)
	}
	assertFilesEqual(t, one, seventeen)
	got := readResolvedWayNodeRawTestFile(t, one)
	if len(got) != len(records) {
		t.Fatalf("sorted record count = %d, want %d", len(got), len(records))
	}
	for index, record := range got {
		if record.Occurrence != uint64(index) {
			t.Fatalf("record %d occurrence = %d", index, record.Occurrence)
		}
	}
	assertDirectoryEmpty(t, oneRuns)
	assertDirectoryEmpty(t, seventeenRuns)
}

func TestSortResolvedWayNodesRejectsGapsDuplicatesTruncationAndCorruption(t *testing.T) {
	tests := []struct {
		name      string
		write     func(*testing.T, string)
		wantError error
		wantText  string
	}{
		{
			name: "initial gap",
			write: func(t *testing.T, path string) {
				writeResolvedWayNodeTestFile(t, path, []resolvedWayNode{resolvedTestRecord(1, 1)})
			},
			wantText: "occurrence gap",
		},
		{
			name: "middle gap",
			write: func(t *testing.T, path string) {
				writeResolvedWayNodeTestFile(t, path, []resolvedWayNode{resolvedTestRecord(2, 2), resolvedTestRecord(0, 0)})
			},
			wantText: "occurrence gap",
		},
		{
			name: "duplicate",
			write: func(t *testing.T, path string) {
				writeResolvedWayNodeTestFile(t, path, []resolvedWayNode{
					resolvedTestRecord(1, 2), resolvedTestRecord(0, 0), resolvedTestRecord(1, 1),
				})
			},
			wantText: "duplicate or decreasing",
		},
		{
			name: "truncation",
			write: func(t *testing.T, path string) {
				if err := os.WriteFile(path, make([]byte, resolvedWayNodeRecordBytes-1), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: ErrTruncatedFixedRecord,
		},
		{
			name: "coordinates",
			write: func(t *testing.T, path string) {
				writeResolvedWayNodeTestFile(t, path, []resolvedWayNode{resolvedTestRecord(0, 1)})
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				binary.BigEndian.PutUint64(data[16:24], math.Float64bits(math.NaN()))
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: worldgraph.ErrInvalidChunk,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			input := filepath.Join(root, "input.bin")
			output := filepath.Join(root, "output.bin")
			runs := filepath.Join(root, "runs")
			test.write(t, input)
			err := sortResolvedWayNodes(context.Background(), input, output, runs, 1)
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("sortResolvedWayNodes() error = %v, want %v", err, test.wantError)
			}
			if test.wantText != "" && (err == nil || !strings.Contains(err.Error(), test.wantText)) {
				t.Fatalf("sortResolvedWayNodes() error = %v, want text %q", err, test.wantText)
			}
			assertPathMissing(t, output)
			assertDirectoryEmpty(t, runs)
		})
	}
}

func TestResolvedWayNodeReaderAndReplayValidateSequenceAndPolarNodes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "resolved.bin")
	owner, err := worldgraph.TileForPosition(0, 0, worldgraph.GlobalRoutingZoom)
	if err != nil {
		t.Fatal(err)
	}
	records := []resolvedWayNode{
		{Occurrence: 0, Node: worldgraph.Node{ID: -1, Lon: -180, Lat: -90, Owner: owner}},
		{Occurrence: 1, Node: worldgraph.Node{ID: 2, Lon: 180, Lat: 90, Owner: owner}},
	}
	writeResolvedWayNodeTestFile(t, path, records)

	reader, err := openResolvedWayNodeReader(path)
	if err != nil {
		t.Fatal(err)
	}
	for index := range records {
		record, ok, err := reader.Next()
		if err != nil || !ok {
			t.Fatalf("Next(%d) = %+v, %v, %v", index, record, ok, err)
		}
		if record.Occurrence != uint64(index) || record.Node.ID != records[index].Node.ID || record.Node.Owner != (worldgraph.TileID{}) {
			t.Fatalf("Next(%d) record = %+v", index, record)
		}
	}
	if record, ok, err := reader.Next(); err != nil || ok || record != (resolvedWayNode{}) {
		t.Fatalf("Next(EOF) = %+v, %v, %v", record, ok, err)
	}
	if _, ok, err := reader.Next(); err != nil || ok {
		t.Fatalf("Next(repeated EOF) = %v, %v", ok, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reader.Next(); err == nil {
		t.Fatal("Next(closed) error = nil")
	}

	var replayed []resolvedWayNode
	if err := replayResolvedWayNodes(context.Background(), path, func(record resolvedWayNode) error {
		replayed = append(replayed, record)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, want := resolvedNodeIDs(replayed), []int64{-1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replayed IDs = %v, want %v", got, want)
	}
}

func TestReplayResolvedWayNodesRejectsOccurrenceAndRecordCorruption(t *testing.T) {
	tests := []struct {
		name      string
		records   []resolvedWayNode
		mutate    func(*testing.T, string)
		wantError error
		wantText  string
		consumed  int
	}{
		{name: "initial gap", records: []resolvedWayNode{resolvedTestRecord(1, 1)}, wantText: "occurrence gap"},
		{name: "middle gap", records: []resolvedWayNode{resolvedTestRecord(0, 0), resolvedTestRecord(2, 2)}, wantText: "occurrence gap", consumed: 1},
		{name: "duplicate", records: []resolvedWayNode{resolvedTestRecord(0, 0), resolvedTestRecord(0, 1)}, wantText: "duplicate or decreasing", consumed: 1},
		{
			name:    "coordinates",
			records: []resolvedWayNode{resolvedTestRecord(0, 0)},
			mutate: func(t *testing.T, path string) {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				binary.BigEndian.PutUint64(data[24:32], math.Float64bits(91))
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: worldgraph.ErrInvalidChunk,
		},
		{
			name: "truncation",
			mutate: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte{1}, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: ErrTruncatedFixedRecord,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "resolved.bin")
			writeResolvedWayNodeTestFile(t, path, test.records)
			if test.mutate != nil {
				test.mutate(t, path)
			}
			consumed := 0
			err := replayResolvedWayNodes(context.Background(), path, func(resolvedWayNode) error {
				consumed++
				return nil
			})
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("replayResolvedWayNodes() error = %v, want %v", err, test.wantError)
			}
			if test.wantText != "" && (err == nil || !strings.Contains(err.Error(), test.wantText)) {
				t.Fatalf("replayResolvedWayNodes() error = %v, want text %q", err, test.wantText)
			}
			if consumed != test.consumed {
				t.Fatalf("consumer calls = %d, want %d", consumed, test.consumed)
			}
		})
	}
}

func TestReferenceJoinCancellationLeavesNoOutputsOrRuns(t *testing.T) {
	root := t.TempDir()
	ways := filepath.Join(root, "ways.spool")
	writeWaySpoolTestFile(t, ways, []Way{{ID: 1, NodeIDs: []int64{1, 2}, Tags: map[string]string{"highway": "road"}}}, DefaultMaxWayNodes)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := spoolWayNodeRequests(ctx, ways, filepath.Join(root, "requests.raw"), DefaultMaxWayNodes); !errors.Is(err, context.Canceled) {
		t.Fatalf("spool cancellation error = %v", err)
	}
	assertPathMissing(t, filepath.Join(root, "requests.raw"))

	requests := filepath.Join(root, "requests.sorted")
	writeWayNodeRequestTestFile(t, requests, []wayNodeRequest{{NodeID: 1, Occurrence: 0}})
	nodes := filepath.Join(root, "nodes.idx")
	writeGlobalNodeTestFile(t, nodes, []globalNodeRecord{globalNodeTestRecord(t, 1, 0, 0)})
	if _, err := joinWayNodeRequests(ctx, requests, nodes, filepath.Join(root, "resolved.bin")); !errors.Is(err, context.Canceled) {
		t.Fatalf("join cancellation error = %v", err)
	}
	assertPathMissing(t, filepath.Join(root, "resolved.bin"))

	input := filepath.Join(root, "resolved.raw")
	writeResolvedWayNodeTestFile(t, input, []resolvedWayNode{resolvedTestRecord(0, 1)})
	runs := filepath.Join(root, "resolved-runs")
	if err := sortResolvedWayNodes(ctx, input, filepath.Join(root, "resolved.sorted"), runs, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("resolved sort cancellation error = %v", err)
	}
	assertPathMissing(t, filepath.Join(root, "resolved.sorted"))
	assertDirectoryEmpty(t, runs)
	if err := replayResolvedWayNodes(ctx, input, func(resolvedWayNode) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("replay cancellation error = %v", err)
	}
}

func writeWaySpoolTestFile(t *testing.T, path string, ways []Way, maxWayNodes int) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for _, way := range ways {
		if err := writeWaySpoolRecord(file, way, maxWayNodes); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeWayNodeRequestTestFile(t *testing.T, path string, records []wayNodeRequest) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if err := writeWayNodeRequestRecord(file, record); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func readWayNodeRequestTestFile(t *testing.T, path string) []wayNodeRequest {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var records []wayNodeRequest
	for {
		record, ok, err := readWayNodeRequestRecord(file)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return records
		}
		records = append(records, record)
	}
}

func writeResolvedWayNodeTestFile(t *testing.T, path string, records []resolvedWayNode) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if err := writeResolvedWayNodeRecord(file, record); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func readResolvedWayNodeRawTestFile(t *testing.T, path string) []resolvedWayNode {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var records []resolvedWayNode
	for {
		record, ok, err := readResolvedWayNodeRecord(file)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return records
		}
		records = append(records, record)
	}
}

func resolvedTestRecord(occurrence uint64, id int64) resolvedWayNode {
	return resolvedWayNode{
		Occurrence: occurrence,
		Node:       worldgraph.Node{ID: id, Lon: float64(id % 180), Lat: float64(id % 90)},
	}
}

func resolvedOccurrences(records []resolvedWayNode) []uint64 {
	occurrences := make([]uint64, len(records))
	for index, record := range records {
		occurrences[index] = record.Occurrence
	}
	return occurrences
}

func resolvedNodeIDs(records []resolvedWayNode) []int64 {
	ids := make([]int64, len(records))
	for index, record := range records {
		ids[index] = record.Node.ID
	}
	return ids
}

func assertFilesEqual(t *testing.T, left, right string) {
	t.Helper()
	leftBytes, err := os.ReadFile(left)
	if err != nil {
		t.Fatal(err)
	}
	rightBytes, err := os.ReadFile(right)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leftBytes, rightBytes) {
		t.Fatalf("files %q and %q differ", left, right)
	}
}

func assertBuilderFileModeAndSize(t *testing.T, path string, mode os.FileMode, size int64) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), mode)
	}
	if info.Size() != size {
		t.Fatalf("%s size = %d, want %d", path, info.Size(), size)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q exists or stat failed: %v", path, err)
	}
}

type cancelAfterChecksContext struct {
	context.Context
	remaining int
	canceled  bool
}

func (ctx *cancelAfterChecksContext) Err() error {
	if ctx.canceled {
		return context.Canceled
	}
	if ctx.remaining == 0 {
		ctx.canceled = true
		return context.Canceled
	}
	ctx.remaining--
	return nil
}

func (ctx *cancelAfterChecksContext) String() string {
	return fmt.Sprintf("cancel after %d checks", ctx.remaining)
}
