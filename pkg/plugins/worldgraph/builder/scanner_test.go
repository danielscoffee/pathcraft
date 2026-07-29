package builder

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
	paul "github.com/paulmach/osm"
)

const seamFixtureSHA256 = "06db561d19ca5a96873b3a820ab2ff61cd8fe503f8af74a3bde899423aca32ba"

func TestScanNodesAndWaysSupportsTwoPasses(t *testing.T) {
	path := seamFixturePath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != seamFixtureSHA256 {
		t.Fatalf("fixture SHA-256 = %s, want %s; regenerate with go run ./scripts/generate-worldgraph-fixture.go", got, seamFixtureSHA256)
	}

	var batches [][]worldgraph.Node
	if err := ScanNodes(context.Background(), path, func(nodes []worldgraph.Node) error {
		batches = append(batches, nodes)
		return nil
	}); err != nil {
		t.Fatalf("ScanNodes() error = %v", err)
	}
	if len(batches) != 1 || len(batches[0]) != 3 {
		t.Fatalf("node batches = %+v, want one batch of three", batches)
	}
	if got := []int64{batches[0][0].ID, batches[0][1].ID, batches[0][2].ID}; !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("node IDs = %v", got)
	}
	if !(batches[0][0].Lon < 12.568359375 && batches[0][1].Lon > 12.568359375) {
		t.Fatalf("fixture does not cross zoom-12 seam: %+v", batches[0])
	}

	var ways []Way
	if err := ScanWays(context.Background(), path, func(way Way) error {
		ways = append(ways, way)
		return nil
	}); err != nil {
		t.Fatalf("ScanWays() error = %v", err)
	}
	if len(ways) != 2 {
		t.Fatalf("ways = %+v, want two", ways)
	}
	if ways[0].ID != 100 || !reflect.DeepEqual(ways[0].NodeIDs, []int64{1, 2, 3}) ||
		ways[0].Tags["highway"] != "residential" || ways[0].Tags["name"] != "Seam Street" {
		t.Fatalf("first way = %+v", ways[0])
	}
	if ways[1].ID != 101 || !reflect.DeepEqual(ways[1].NodeIDs, []int64{3, 2}) || ways[1].Tags["highway"] != "footway" {
		t.Fatalf("second way = %+v", ways[1])
	}
}

func TestScanRejectsMalformedPBF(t *testing.T) {
	data, err := os.ReadFile(seamFixturePath())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "truncated.osm.pbf")
	if err := os.WriteFile(path, data[:len(data)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ScanNodes(context.Background(), path, func([]worldgraph.Node) error { return nil }); err == nil {
		t.Fatal("ScanNodes() error = nil, want malformed PBF rejection")
	}
}

func TestScanHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ScanNodes(ctx, seamFixturePath(), func([]worldgraph.Node) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("ScanNodes() error = %v, want context.Canceled", err)
	}
	if err := ScanWays(ctx, seamFixturePath(), func(Way) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("ScanWays() error = %v, want context.Canceled", err)
	}
}

func TestScanNodesRejectsInvalidCoordinates(t *testing.T) {
	tests := []*paul.Node{
		{ID: 1, Lon: math.NaN(), Lat: 0},
		{ID: 1, Lon: 181, Lat: 0},
		{ID: 1, Lon: 0, Lat: worldgraph.MaxMercatorLatitude + 1},
	}
	for _, node := range tests {
		if _, err := copyNode(node); err == nil {
			t.Fatalf("copyNode(%+v) error = nil, want coordinate rejection", node)
		}
	}
}

func TestScanWaysRejectsExcessiveNodeCount(t *testing.T) {
	err := scanWays(context.Background(), seamFixturePath(), 2, func(Way) error { return nil })
	if !errors.Is(err, ErrWayNodeLimit) {
		t.Fatalf("scanWays() error = %v, want ErrWayNodeLimit", err)
	}
}

func seamFixturePath() string {
	return filepath.Join("testdata", "seam.osm.pbf")
}
