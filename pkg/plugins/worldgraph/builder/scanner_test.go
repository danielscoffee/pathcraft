package builder

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
	paul "github.com/paulmach/osm"
	"google.golang.org/protobuf/encoding/protowire"
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

func TestScanWaysRejectsOutOfRangeStringTableIndex(t *testing.T) {
	stringTable := testPBFBytesField(nil, 1, nil)
	way := testPBFVarintField(nil, 1, 1)
	way = testPBFBytesField(way, 2, testPBFPackedVarints(9))
	way = testPBFBytesField(way, 3, testPBFPackedVarints(0))
	way = testPBFBytesField(way, 8, testPBFPackedSInt64(1, 1))
	group := testPBFBytesField(nil, 3, way)
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 2, group)
	path := writeTestPBF(t, testPBFFileBlock("OSMData", block))

	if err := ScanWays(context.Background(), path, func(Way) error { return nil }); err == nil {
		t.Fatal("ScanWays() error = nil, want invalid string-table index rejection")
	}
}

func TestScanWaysRejectsRepeatedReferenceFields(t *testing.T) {
	stringTable := testPBFBytesField(nil, 1, nil)
	way := testPBFVarintField(nil, 1, 1)
	way = testPBFBytesField(way, 8, testPBFPackedSInt64(1))
	way = testPBFBytesField(way, 8, testPBFPackedSInt64(1, 1))
	group := testPBFBytesField(nil, 3, way)
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 2, group)
	path := writeTestPBF(t, testPBFFileBlock("OSMData", block))

	if err := ScanWays(context.Background(), path, func(Way) error { return nil }); err == nil {
		t.Fatal("ScanWays() error = nil, want repeated reference rejection")
	}
}

func TestScanAcceptsPrimitiveGroupBeforeStringTable(t *testing.T) {
	stringTable := testPBFBytesField(nil, 1, nil)
	way := testPBFVarintField(nil, 1, 1)
	way = testPBFBytesField(way, 8, testPBFPackedSInt64(1, 1))
	group := testPBFBytesField(nil, 3, way)
	block := testPBFBytesField(nil, 2, group)
	block = testPBFBytesField(block, 1, stringTable)
	path := writeTestPBF(t, testPBFFileBlock("OSMData", block))

	count := 0
	if err := ScanWays(context.Background(), path, func(Way) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("way count = %d, want one", count)
	}
}

func TestScanAcceptsBoundedZlibPBF(t *testing.T) {
	stringTable := testPBFBytesField(nil, 1, nil)
	way := testPBFVarintField(nil, 1, 1)
	way = testPBFBytesField(way, 8, testPBFPackedSInt64(1, 1))
	group := testPBFBytesField(nil, 3, way)
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 2, group)
	path := writeTestPBF(t, testPBFZlibFileBlock("OSMData", block))

	count := 0
	if err := ScanWays(context.Background(), path, func(Way) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("way count = %d, want one", count)
	}
}

func TestScanRejectsOversizedUncompressedBlock(t *testing.T) {
	const declaredSize = 65 << 20
	blob := testPBFVarintField(nil, 2, declaredSize)
	blob = testPBFBytesField(blob, 3, []byte{0})
	path := writeTestPBF(t, testPBFRawFileBlock("OSMData", blob))

	err := ScanWays(context.Background(), path, func(Way) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "uncompressed PBF block") {
		t.Fatalf("ScanWays() error = %v, want uncompressed block limit", err)
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

func writeTestPBF(t *testing.T, dataBlock []byte) string {
	t.Helper()
	header := testPBFBytesField(nil, 4, []byte("OsmSchema-V0.6"))
	header = testPBFBytesField(header, 4, []byte("DenseNodes"))
	data := append(testPBFFileBlock("OSMHeader", header), dataBlock...)
	path := filepath.Join(t.TempDir(), "test.osm.pbf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testPBFFileBlock(kind string, payload []byte) []byte {
	blob := testPBFBytesField(nil, 1, payload)
	blob = testPBFVarintField(blob, 2, uint64(len(payload)))
	return testPBFRawFileBlock(kind, blob)
}

func testPBFZlibFileBlock(kind string, payload []byte) []byte {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	blob := testPBFVarintField(nil, 2, uint64(len(payload)))
	blob = testPBFBytesField(blob, 3, compressed.Bytes())
	return testPBFRawFileBlock(kind, blob)
}

func testPBFRawFileBlock(kind string, blob []byte) []byte {
	header := testPBFBytesField(nil, 1, []byte(kind))
	header = testPBFVarintField(header, 3, uint64(len(blob)))
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(len(header)))
	data = append(data, header...)
	return append(data, blob...)
}

func testPBFBytesField(data []byte, number protowire.Number, value []byte) []byte {
	data = protowire.AppendTag(data, number, protowire.BytesType)
	return protowire.AppendBytes(data, value)
}

func testPBFVarintField(data []byte, number protowire.Number, value uint64) []byte {
	data = protowire.AppendTag(data, number, protowire.VarintType)
	return protowire.AppendVarint(data, value)
}

func testPBFPackedVarints(values ...uint64) []byte {
	var data []byte
	for _, value := range values {
		data = protowire.AppendVarint(data, value)
	}
	return data
}

func testPBFPackedSInt64(values ...int64) []byte {
	var data []byte
	for _, value := range values {
		data = protowire.AppendVarint(data, protowire.EncodeZigZag(value))
	}
	return data
}
