package builder

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWaySpoolRoundTripIsDeterministic(t *testing.T) {
	way := Way{
		ID:      -7,
		NodeIDs: []int64{-2, 5, 9},
		Tags: map[string]string{
			"highway": "residential", "name": "Main Street", "oneway": "yes",
			"junction": "roundabout", "access": "yes", "foot": "designated",
			"vehicle": "yes", "motor_vehicle": "yes", "motorcar": "yes", "service": "alley",
		},
	}
	var first, second bytes.Buffer
	if err := writeWaySpoolRecord(&first, way, DefaultMaxWayNodes); err != nil {
		t.Fatal(err)
	}
	if err := writeWaySpoolRecord(&second, way, DefaultMaxWayNodes); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("way spool encoding is not deterministic")
	}
	got, ok, err := readWaySpoolRecord(bytes.NewReader(first.Bytes()), DefaultMaxWayNodes)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !reflect.DeepEqual(got, way) {
		t.Fatalf("readWaySpoolRecord() = %+v, %v; want %+v", got, ok, way)
	}
	if _, ok, err := readWaySpoolRecord(bytes.NewReader(nil), DefaultMaxWayNodes); err != nil || ok {
		t.Fatalf("empty read = %v, %v", ok, err)
	}
}

func TestWaySpoolRejectsMalformedAndExcessiveRecords(t *testing.T) {
	valid := Way{ID: 1, NodeIDs: []int64{1, 2}, Tags: map[string]string{"highway": "residential"}}
	var encoded bytes.Buffer
	if err := writeWaySpoolRecord(&encoded, valid, DefaultMaxWayNodes); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readWaySpoolRecord(bytes.NewReader(encoded.Bytes()[:encoded.Len()-1]), DefaultMaxWayNodes); !errors.Is(err, ErrTruncatedFixedRecord) {
		t.Fatalf("truncated read error = %v, want ErrTruncatedFixedRecord", err)
	}
	var oversized bytes.Buffer
	if err := binary.Write(&oversized, binary.BigEndian, uint32(maxWaySpoolRecordBytes+1)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readWaySpoolRecord(&oversized, DefaultMaxWayNodes); err == nil {
		t.Fatal("oversized read error = nil")
	}

	tests := []struct {
		name string
		way  Way
	}{
		{name: "too few nodes", way: Way{ID: 1, NodeIDs: []int64{1}, Tags: map[string]string{"highway": "residential"}}},
		{name: "too many nodes", way: Way{ID: 1, NodeIDs: make([]int64, 3), Tags: map[string]string{"highway": "residential"}}},
		{name: "highway text", way: Way{ID: 1, NodeIDs: []int64{1, 2}, Tags: map[string]string{"highway": strings.Repeat("h", 257)}}},
		{name: "name text", way: Way{ID: 1, NodeIDs: []int64{1, 2}, Tags: map[string]string{"highway": "road", "name": strings.Repeat("n", 1025)}}},
		{name: "policy text", way: Way{ID: 1, NodeIDs: []int64{1, 2}, Tags: map[string]string{"highway": "road", "access": strings.Repeat("a", maxWaySpoolPolicyBytes+1)}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limit := DefaultMaxWayNodes
			if test.name == "too many nodes" {
				limit = 2
			}
			if err := writeWaySpoolRecord(&bytes.Buffer{}, test.way, limit); err == nil {
				t.Fatal("writeWaySpoolRecord() error = nil")
			}
		})
	}
}

func TestWaySpoolReplayHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ways.spool")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeWaySpoolRecord(file, Way{ID: 1, NodeIDs: []int64{1, 2}, Tags: map[string]string{"highway": "residential"}}, DefaultMaxWayNodes); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := replayWaySpool(ctx, path, DefaultMaxWayNodes, func(Way) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("replayWaySpool() error = %v, want context.Canceled", err)
	}
}

func TestWaySpoolFirstGlobalPassWritesWaysAndReferences(t *testing.T) {
	source, err := openPBFSource(context.Background(), seamFixturePath(), DefaultMaxWayNodes)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	root := t.TempDir()
	waysPath := filepath.Join(root, "ways.spool")
	referencesPath := filepath.Join(root, "refs.raw")
	ways, references, err := spoolGlobalWaysAndReferences(context.Background(), source, waysPath, referencesPath)
	if err != nil {
		t.Fatal(err)
	}
	if ways != 2 || references != 5 {
		t.Fatalf("spool counts = %d ways, %d references", ways, references)
	}
	var gotWays []Way
	if err := replayWaySpool(context.Background(), waysPath, DefaultMaxWayNodes, func(way Way) error {
		gotWays = append(gotWays, way)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(gotWays) != 2 || gotWays[0].ID != 100 || gotWays[1].ID != 101 {
		t.Fatalf("spooled ways = %+v", gotWays)
	}
	if got := readInt64TestFile(t, referencesPath); !reflect.DeepEqual(got, []int64{1, 2, 3, 3, 2}) {
		t.Fatalf("raw references = %v", got)
	}
	for _, path := range []string{waysPath, referencesPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
}
