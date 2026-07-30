package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
	bolt "go.etcd.io/bbolt"
)

func TestNodeIndexOrdersSignedIDs(t *testing.T) {
	index := openTestNodeIndex(t)
	nodes := []worldgraph.Node{
		{ID: 1, Lon: 1, Lat: 1},
		{ID: -2, Lon: -2, Lat: -2},
		{ID: 0, Lon: 0, Lat: 0},
		{ID: -1, Lon: -1, Lat: -1},
	}
	if err := index.PutBatch(context.Background(), nodes); err != nil {
		t.Fatal(err)
	}

	var got []int64
	if err := index.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(nodeBucket).Cursor()
		for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
			got = append(got, decodeNodeKey(key))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []int64{-2, -1, 0, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered IDs = %v, want %v", got, want)
	}
}

func TestNodeIndexPutBatchAndGet(t *testing.T) {
	index := openTestNodeIndex(t)
	want := []worldgraph.Node{
		{ID: -7, Lon: -179.5, Lat: -45.25, Owner: worldgraph.TileID{Z: 12, X: 5, Y: 6}},
		{ID: 9, Lon: 12.5683, Lat: 55.6761, Owner: worldgraph.TileID{Z: 12, X: 2191, Y: 1282}},
	}
	if err := index.PutBatch(context.Background(), want); err != nil {
		t.Fatalf("PutBatch() error = %v", err)
	}
	for _, node := range want {
		got, ok, err := index.Get(node.ID)
		if err != nil {
			t.Fatalf("Get(%d) error = %v", node.ID, err)
		}
		if !ok || got != node {
			t.Fatalf("Get(%d) = %+v, %v, want %+v, true", node.ID, got, ok, node)
		}
	}

	if err := index.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(nodeBucket).Get(nodeKey(want[0].ID))
		if len(value) != nodeValueSize {
			t.Fatalf("stored value length = %d, want %d", len(value), nodeValueSize)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNodeIndexGetAbsentID(t *testing.T) {
	index := openTestNodeIndex(t)
	if node, ok, err := index.Get(404); err != nil || ok || node != (worldgraph.Node{}) {
		t.Fatalf("Get(absent) = %+v, %v, %v", node, ok, err)
	}
}

func TestNodeIndexPutBatchHonorsCancellation(t *testing.T) {
	index := openTestNodeIndex(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := index.PutBatch(ctx, []worldgraph.Node{{ID: 1}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("PutBatch() error = %v, want context.Canceled", err)
	}
	if _, ok, err := index.Get(1); err != nil || ok {
		t.Fatalf("cancelled write persisted: ok=%v err=%v", ok, err)
	}
}

func TestNodeIndexUsesSecureMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodes.db")
	index, err := OpenNodeIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("node index mode = %o, want 600", got)
	}
}

func TestNodeIndexOpenHasBoundedLockWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodes.db")
	first, err := OpenNodeIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	started := time.Now()
	second, err := OpenNodeIndex(path)
	elapsed := time.Since(started)
	if second != nil {
		_ = second.Close()
	}
	if err == nil {
		t.Fatal("second OpenNodeIndex() error = nil, want lock timeout")
	}
	if elapsed < 500*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("lock wait = %v, want bounded near one second", elapsed)
	}
}

func openTestNodeIndex(t *testing.T) *NodeIndex {
	t.Helper()
	index, err := OpenNodeIndex(filepath.Join(t.TempDir(), "nodes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := index.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return index
}
