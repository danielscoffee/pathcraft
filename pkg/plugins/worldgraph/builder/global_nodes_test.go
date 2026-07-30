package builder

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

func TestGlobalNodeIndexReadsFixedRecordsConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodes.idx")
	records := []globalNodeRecord{
		globalNodeTestRecord(t, -2, -70, -10),
		globalNodeTestRecord(t, 5, 12, 55),
		globalNodeTestRecord(t, 9, 139, 35),
	}
	writeGlobalNodeTestFile(t, path, records)
	index, err := openGlobalNodeIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	for _, record := range records {
		node, found, err := index.Get(record.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := worldgraph.Node{ID: record.ID, Lon: record.Lon, Lat: record.Lat, Owner: record.Owner}
		if !found || node != want {
			t.Fatalf("Get(%d) = %+v, %v; want %+v", record.ID, node, found, want)
		}
	}
	if node, found, err := index.Get(6); err != nil || found || node != (worldgraph.Node{}) {
		t.Fatalf("Get(missing) = %+v, %v, %v", node, found, err)
	}

	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, found, err := index.Get(5); err != nil || !found {
				t.Errorf("concurrent Get() = %v, %v", found, err)
			}
		}()
	}
	wait.Wait()
}

func TestGlobalNodeIndexRejectsMalformedRecords(t *testing.T) {
	t.Run("size", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nodes.idx")
		if err := os.WriteFile(path, []byte{1}, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := openGlobalNodeIndex(path); !errors.Is(err, ErrTruncatedFixedRecord) {
			t.Fatalf("openGlobalNodeIndex() error = %v, want ErrTruncatedFixedRecord", err)
		}
	})

	t.Run("coordinates", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nodes.idx")
		record := globalNodeTestRecord(t, 1, 0, 0)
		writeGlobalNodeTestFile(t, path, []globalNodeRecord{record})
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		binary.BigEndian.PutUint64(data[8:16], math.Float64bits(math.NaN()))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := openGlobalNodeIndex(path); !errors.Is(err, worldgraph.ErrInvalidChunk) {
			t.Fatalf("openGlobalNodeIndex() error = %v, want ErrInvalidChunk", err)
		}
	})

	t.Run("order", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nodes.idx")
		writeGlobalNodeTestFile(t, path, []globalNodeRecord{
			globalNodeTestRecord(t, 2, 0, 0),
			globalNodeTestRecord(t, 1, 1, 1),
		})
		if _, err := openGlobalNodeIndex(path); err == nil {
			t.Fatal("openGlobalNodeIndex() error = nil")
		}
	})
}

func TestGlobalNodeSelectionMergesMonotonicNodes(t *testing.T) {
	root := t.TempDir()
	references := filepath.Join(root, "refs.sorted")
	writeInt64TestFile(t, references, []int64{-2, 2, 5})
	output := filepath.Join(root, "nodes.idx")
	nodes := []worldgraph.Node{
		{ID: -3, Lon: -80, Lat: -20},
		{ID: -2, Lon: -70, Lat: -10},
		{ID: 0, Lon: 0, Lat: 0},
		{ID: 2, Lon: 12, Lat: 55},
		{ID: 5, Lon: 139, Lat: 35},
		{ID: 9, Lon: 150, Lat: 40},
	}
	if err := writeSelectedGlobalNodes(context.Background(), references, output, func(consume func([]worldgraph.Node) error) error {
		if err := consume(nodes[:3]); err != nil {
			return err
		}
		return consume(nodes[3:])
	}); err != nil {
		t.Fatal(err)
	}
	index, err := openGlobalNodeIndex(output)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	for _, id := range []int64{-2, 2, 5} {
		if _, found, err := index.Get(id); err != nil || !found {
			t.Fatalf("Get(%d) = %v, %v", id, found, err)
		}
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("output mode = %o, want 600", info.Mode().Perm())
	}
}

func TestGlobalNodeSelectionRejectsInvalidStreams(t *testing.T) {
	tests := []struct {
		name       string
		references []int64
		nodes      []worldgraph.Node
	}{
		{
			name:       "missing reference",
			references: []int64{2},
			nodes:      []worldgraph.Node{{ID: 1, Lon: 0, Lat: 0}, {ID: 3, Lon: 1, Lat: 1}},
		},
		{
			name:       "decreasing nodes",
			references: []int64{2},
			nodes:      []worldgraph.Node{{ID: 2, Lon: 0, Lat: 0}, {ID: 1, Lon: 1, Lat: 1}},
		},
		{
			name:       "conflicting duplicate",
			references: []int64{2},
			nodes:      []worldgraph.Node{{ID: 2, Lon: 0, Lat: 0}, {ID: 2, Lon: 1, Lat: 1}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			references := filepath.Join(root, "refs.sorted")
			writeInt64TestFile(t, references, test.references)
			output := filepath.Join(root, "nodes.idx")
			err := writeSelectedGlobalNodes(context.Background(), references, output, func(consume func([]worldgraph.Node) error) error {
				return consume(test.nodes)
			})
			if err == nil {
				t.Fatal("writeSelectedGlobalNodes() error = nil")
			}
			if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("invalid output exists: %v", statErr)
			}
		})
	}
}

func globalNodeTestRecord(t *testing.T, id int64, lon, lat float64) globalNodeRecord {
	t.Helper()
	owner, err := worldgraph.TileForPosition(lon, lat, worldgraph.GlobalRoutingZoom)
	if err != nil {
		t.Fatal(err)
	}
	return globalNodeRecord{ID: id, Lon: lon, Lat: lat, Owner: owner}
}

func writeGlobalNodeTestFile(t *testing.T, path string, records []globalNodeRecord) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if err := writeGlobalNodeRecord(file, record); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
