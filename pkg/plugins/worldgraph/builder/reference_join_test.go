package builder

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReferenceJoinResolvesRepeatedSignedAndPolarNodes(t *testing.T) {
	root := t.TempDir()
	waysPath := writeReferenceJoinWays(t, root, []Way{
		{ID: 10, NodeIDs: []int64{5, -2, 5}, Tags: map[string]string{"highway": "residential"}},
		{ID: 11, NodeIDs: []int64{9, 5}, Tags: map[string]string{"highway": "footway"}},
	})
	rawRequests := filepath.Join(root, "requests.raw")
	count, err := spoolWayNodeRequests(context.Background(), waysPath, rawRequests, DefaultMaxWayNodes)
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("request count = %d, want 5", count)
	}
	sortedRequests := filepath.Join(root, "requests.sorted")
	if err := sortWayNodeRequests(context.Background(), rawRequests, sortedRequests, filepath.Join(root, "request-runs"), 1); err != nil {
		t.Fatal(err)
	}

	nodesPath := filepath.Join(root, "nodes.idx")
	writeGlobalNodeTestFile(t, nodesPath, []globalNodeRecord{
		globalNodeTestRecord(t, -2, -70, -10),
		globalNodeTestRecord(t, 5, 12, 55),
		{ID: 9, Lon: 0, Lat: 90},
	})
	rawResolved := filepath.Join(root, "resolved.raw")
	resolved, err := joinWayNodeRequests(context.Background(), sortedRequests, nodesPath, rawResolved)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != count {
		t.Fatalf("resolved count = %d, want %d", resolved, count)
	}
	sortedResolved := filepath.Join(root, "resolved.sorted")
	if err := sortResolvedWayNodes(context.Background(), rawResolved, sortedResolved, filepath.Join(root, "resolved-runs"), 1); err != nil {
		t.Fatal(err)
	}

	var got []resolvedWayNode
	if err := replayResolvedWayNodes(context.Background(), sortedResolved, func(record resolvedWayNode) error {
		got = append(got, record)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	gotIDs := make([]int64, len(got))
	for index, record := range got {
		gotIDs[index] = record.NodeID
		if record.Occurrence != uint64(index) {
			t.Fatalf("occurrence[%d] = %d", index, record.Occurrence)
		}
	}
	if want := []int64{5, -2, 5, 9, 5}; !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("resolved IDs = %v, want %v", gotIDs, want)
	}
	if got[3].Lat != 90 {
		t.Fatalf("polar record = %+v", got[3])
	}
	for _, path := range []string{rawRequests, sortedRequests, rawResolved, sortedResolved} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
}

func TestReferenceJoinSortIsDeterministicBeyondMergeFanIn(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "requests.raw")
	file, err := os.OpenFile(input, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxMergeFanIn+7; index++ {
		record := wayNodeRequest{NodeID: int64((index % 9) - 4), Occurrence: uint64(maxMergeFanIn + 6 - index)}
		if err := writeWayNodeRequestRecord(file, record); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "first.sorted")
	second := filepath.Join(root, "second.sorted")
	if err := sortWayNodeRequests(context.Background(), input, first, filepath.Join(root, "runs-a"), 1); err != nil {
		t.Fatal(err)
	}
	if err := sortWayNodeRequests(context.Background(), input, second, filepath.Join(root, "runs-b"), 13); err != nil {
		t.Fatal(err)
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
		t.Fatal("sorted request bytes depend on run size")
	}
	reader := bytes.NewReader(firstBytes)
	var previous wayNodeRequest
	for index := 0; ; index++ {
		record, ok, err := readWayNodeRequestRecord(reader)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if index > 0 && (record.NodeID < previous.NodeID || record.NodeID == previous.NodeID && record.Occurrence < previous.Occurrence) {
			t.Fatalf("requests are not sorted: %+v then %+v", previous, record)
		}
		previous = record
	}
}

func TestReferenceJoinRejectsMissingAndCorruptStreams(t *testing.T) {
	t.Run("missing node", func(t *testing.T) {
		root := t.TempDir()
		requests := filepath.Join(root, "requests.sorted")
		writeWayNodeRequestsForTest(t, requests, []wayNodeRequest{{NodeID: 1, Occurrence: 0}, {NodeID: 3, Occurrence: 1}})
		nodes := filepath.Join(root, "nodes.idx")
		writeGlobalNodeTestFile(t, nodes, []globalNodeRecord{globalNodeTestRecord(t, 1, 0, 0), globalNodeTestRecord(t, 2, 1, 1)})
		if _, err := joinWayNodeRequests(context.Background(), requests, nodes, filepath.Join(root, "resolved")); err == nil {
			t.Fatal("join error = nil, want missing reference")
		}
	})

	t.Run("request order", func(t *testing.T) {
		root := t.TempDir()
		requests := filepath.Join(root, "requests.sorted")
		writeWayNodeRequestsForTest(t, requests, []wayNodeRequest{{NodeID: 1, Occurrence: 1}, {NodeID: 1, Occurrence: 0}})
		nodes := filepath.Join(root, "nodes.idx")
		writeGlobalNodeTestFile(t, nodes, []globalNodeRecord{globalNodeTestRecord(t, 1, 0, 0)})
		if _, err := joinWayNodeRequests(context.Background(), requests, nodes, filepath.Join(root, "resolved")); !errors.Is(err, ErrCorruptReferenceJoin) {
			t.Fatalf("join error = %v, want ErrCorruptReferenceJoin", err)
		}
	})

	t.Run("truncated request", func(t *testing.T) {
		root := t.TempDir()
		input := filepath.Join(root, "requests.raw")
		if err := os.WriteFile(input, []byte{1}, 0o600); err != nil {
			t.Fatal(err)
		}
		err := sortWayNodeRequests(context.Background(), input, filepath.Join(root, "sorted"), filepath.Join(root, "runs"), 1)
		if !errors.Is(err, ErrTruncatedFixedRecord) {
			t.Fatalf("sort error = %v, want ErrTruncatedFixedRecord", err)
		}
	})

	t.Run("occurrence gap", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "resolved.sorted")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range []resolvedWayNode{
			{Occurrence: 0, NodeID: 1, Lon: 0, Lat: 0},
			{Occurrence: 2, NodeID: 2, Lon: 1, Lat: 1},
		} {
			if err := writeResolvedWayNodeRecord(file, record); err != nil {
				t.Fatal(err)
			}
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		reader, err := openResolvedWayNodeReader(path)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		if _, ok, err := reader.Next(); err != nil || !ok {
			t.Fatalf("first Next = %v, %v", ok, err)
		}
		if _, _, err := reader.Next(); !errors.Is(err, ErrCorruptReferenceJoin) {
			t.Fatalf("second Next error = %v, want ErrCorruptReferenceJoin", err)
		}
	})
}

func TestReferenceJoinHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := spoolWayNodeRequests(ctx, "ways", "out", DefaultMaxWayNodes); !errors.Is(err, context.Canceled) {
		t.Fatalf("spool error = %v, want context.Canceled", err)
	}
	if err := sortWayNodeRequests(ctx, "in", "out", "runs", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("sort error = %v, want context.Canceled", err)
	}
	if _, err := joinWayNodeRequests(ctx, "requests", "nodes", "out"); !errors.Is(err, context.Canceled) {
		t.Fatalf("join error = %v, want context.Canceled", err)
	}
	if err := replayResolvedWayNodes(ctx, "resolved", func(resolvedWayNode) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("replay error = %v, want context.Canceled", err)
	}
}

func writeReferenceJoinWays(t *testing.T, root string, ways []Way) string {
	t.Helper()
	path := filepath.Join(root, "ways.spool")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for _, way := range ways {
		if err := writeWaySpoolRecord(file, way, DefaultMaxWayNodes); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeWayNodeRequestsForTest(t *testing.T, path string, records []wayNodeRequest) {
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
