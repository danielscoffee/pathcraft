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

func TestScanWaysRejectsProtobufGroups(t *testing.T) {
	stringTable := testPBFBytesField(nil, 1, nil)
	hiddenNode := protowire.AppendTag(nil, 99, protowire.StartGroupType)
	hiddenNode = testPBFBytesField(hiddenNode, 1, nil)
	hiddenNode = protowire.AppendTag(hiddenNode, 99, protowire.EndGroupType)
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 2, hiddenNode)
	path := writeTestPBF(t, testPBFFileBlock("OSMData", block))

	if err := ScanWays(context.Background(), path, func(Way) error { return nil }); err == nil {
		t.Fatal("ScanWays() error = nil, want protobuf group rejection")
	}
}

func TestScanWaysRejectsWrongWireType(t *testing.T) {
	stringTable := testPBFBytesField(nil, 1, nil)
	way := testPBFBytesField(nil, 1, testPBFBytesField(nil, 2, protowire.AppendVarint(nil, 9)))
	group := testPBFBytesField(nil, 3, way)
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 2, group)
	path := writeTestPBF(t, testPBFFileBlock("OSMData", block))

	if err := ScanWays(context.Background(), path, func(Way) error { return nil }); err == nil {
		t.Fatal("ScanWays() error = nil, want wrong wire-type rejection")
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

func TestScanRejectsWrongPrimitiveBlockScalarWireType(t *testing.T) {
	stringTable := testPBFBytesField(nil, 1, nil)
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 17, nil)
	path := writeTestPBF(t, testPBFFileBlock("OSMData", block))

	if err := ScanNodes(context.Background(), path, func([]worldgraph.Node) error { return nil }); err == nil {
		t.Fatal("ScanNodes() error = nil, want wrong scalar wire-type rejection")
	}
}

func TestValidatePBFAcceptsPlanetSizedDenseBlock(t *testing.T) {
	const nodes = 288_000
	column := bytes.Repeat([]byte{0}, nodes)
	dense := testPBFBytesField(nil, 1, column)
	dense = testPBFBytesField(dense, 8, column)
	dense = testPBFBytesField(dense, 9, column)
	group := testPBFBytesField(nil, 2, dense)
	stringTable := testPBFBytesField(nil, 1, nil)
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 2, group)

	if err := validatePBFPrimitiveBlock(block, DefaultMaxWayNodes); err != nil {
		t.Fatalf("validatePBFPrimitiveBlock() error = %v", err)
	}
}

func TestScanStreamsPlanetSizedDenseBlocks(t *testing.T) {
	const (
		nodesPerBlock = 288_000
		blocks        = 3
	)
	column := bytes.Repeat([]byte{0}, nodesPerBlock)
	dense := testPBFBytesField(nil, 1, column)
	dense = testPBFBytesField(dense, 8, column)
	dense = testPBFBytesField(dense, 9, column)
	group := testPBFBytesField(nil, 2, dense)
	stringTable := testPBFBytesField(nil, 1, nil)
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 2, group)
	encoded := testPBFZlibFileBlock("OSMData", block)
	path := writeTestPBF(t, bytes.Repeat(encoded, blocks))

	count := 0
	if err := ScanNodes(context.Background(), path, func(nodes []worldgraph.Node) error {
		count += len(nodes)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != nodesPerBlock*blocks {
		t.Fatalf("scanned nodes = %d, want %d", count, nodesPerBlock*blocks)
	}
}

func TestScanRejectsCompressedDenseEntityBomb(t *testing.T) {
	column := bytes.Repeat([]byte{0}, maxPBFEntitiesPerBlock+1)
	dense := testPBFBytesField(nil, 1, column)
	dense = testPBFBytesField(dense, 8, column)
	dense = testPBFBytesField(dense, 9, column)
	group := testPBFBytesField(nil, 2, dense)
	stringTable := testPBFBytesField(nil, 1, nil)
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 2, group)
	path := writeTestPBF(t, testPBFZlibFileBlock("OSMData", block))

	if err := ScanNodes(context.Background(), path, func([]worldgraph.Node) error { return nil }); err == nil {
		t.Fatal("ScanNodes() error = nil, want dense entity limit")
	}
}

func TestScanRejectsCompressedHeaderFeatureBomb(t *testing.T) {
	header := make([]byte, 0, 2*(maxPBFHeaderFeatures+1))
	for range maxPBFHeaderFeatures + 1 {
		header = testPBFBytesField(header, 4, nil)
	}
	path := filepath.Join(t.TempDir(), "header-bomb.osm.pbf")
	if err := os.WriteFile(path, testPBFZlibFileBlock("OSMHeader", header), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ScanNodes(context.Background(), path, func([]worldgraph.Node) error { return nil }); err == nil || !strings.Contains(err.Error(), "features") {
		t.Fatalf("ScanNodes() error = %v, want header feature limit", err)
	}
}

func TestValidatePBFRejectsStringTableEntryBomb(t *testing.T) {
	stringTable := make([]byte, 0, 2*(maxPBFStringTableEntries+1))
	for range maxPBFStringTableEntries + 1 {
		stringTable = testPBFBytesField(stringTable, 1, nil)
	}
	block := testPBFBytesField(nil, 1, stringTable)
	if err := validatePBFPrimitiveBlock(block, DefaultMaxWayNodes); err == nil {
		t.Fatal("validatePBFPrimitiveBlock() error = nil, want string-table limit")
	}
}

func TestValidatePBFRejectsOversizedString(t *testing.T) {
	stringTable := testPBFBytesField(nil, 1, nil)
	stringTable = testPBFBytesField(stringTable, 1, bytes.Repeat([]byte{'x'}, maxPBFStringLength+1))
	block := testPBFBytesField(nil, 1, stringTable)
	if err := validatePBFPrimitiveBlock(block, DefaultMaxWayNodes); err == nil {
		t.Fatal("validatePBFPrimitiveBlock() error = nil, want string-size limit")
	}
}

func TestValidatePBFRejectsExcessiveWayTags(t *testing.T) {
	stringTable := testPBFBytesField(nil, 1, nil)
	stringTable = testPBFBytesField(stringTable, 1, []byte("tag"))
	indexes := make([]byte, 0, maxPBFTagsPerEntity+1)
	for range maxPBFTagsPerEntity + 1 {
		indexes = protowire.AppendVarint(indexes, 1)
	}
	way := testPBFVarintField(nil, 1, 1)
	way = testPBFBytesField(way, 2, indexes)
	way = testPBFBytesField(way, 3, indexes)
	group := testPBFBytesField(nil, 3, way)
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 2, group)
	if err := validatePBFPrimitiveBlock(block, DefaultMaxWayNodes); err == nil {
		t.Fatal("validatePBFPrimitiveBlock() error = nil, want tag limit")
	}
}

func TestValidatePBFRejectsMismatchedDenseInfoColumns(t *testing.T) {
	info := testPBFBytesField(nil, 1, testPBFPackedVarints(1, 1))
	dense := testPBFBytesField(nil, 1, testPBFPackedSInt64(1))
	dense = testPBFBytesField(dense, 5, info)
	dense = testPBFBytesField(dense, 8, testPBFPackedSInt64(1))
	dense = testPBFBytesField(dense, 9, testPBFPackedSInt64(1))
	group := testPBFBytesField(nil, 2, dense)
	stringTable := testPBFBytesField(nil, 1, nil)
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 2, group)
	if err := validatePBFPrimitiveBlock(block, DefaultMaxWayNodes); err == nil {
		t.Fatal("validatePBFPrimitiveBlock() error = nil, want dense-info length rejection")
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

func TestValidatedScannerUsesPrivateSnapshot(t *testing.T) {
	source := filepath.Join(t.TempDir(), "input.osm.pbf")
	original, err := os.ReadFile(seamFixturePath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := scanValidatedPBFSnapshot(context.Background(), source, DefaultMaxWayNodes, func(snapshot string) error {
		if snapshot == source {
			t.Fatal("scanner reused mutable source path")
		}
		if err := os.WriteFile(source, []byte("replaced"), 0o600); err != nil {
			return err
		}
		got, err := os.ReadFile(snapshot)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, original) {
			t.Fatal("private scanner snapshot changed with source")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestScanPropagatesConsumerErrors(t *testing.T) {
	want := errors.New("stop scan")
	if err := ScanNodes(context.Background(), seamFixturePath(), func([]worldgraph.Node) error { return want }); !errors.Is(err, want) {
		t.Fatalf("ScanNodes() error = %v, want consumer error", err)
	}
	if err := ScanWays(context.Background(), seamFixturePath(), func(Way) error { return want }); !errors.Is(err, want) {
		t.Fatalf("ScanWays() error = %v, want consumer error", err)
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

func TestCopyGlobalNodeAcceptsPolarCoordinates(t *testing.T) {
	node, err := copyGlobalNode(&paul.Node{ID: 1, Lon: 0, Lat: 90})
	if err != nil {
		t.Fatal(err)
	}
	if node.Lat != 90 {
		t.Fatalf("node = %+v", node)
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

func FuzzAcceptedPBFPrimitiveBlocksDoNotPanicDecoder(f *testing.F) {
	stringTable := testPBFBytesField(nil, 1, nil)
	way := testPBFVarintField(nil, 1, 1)
	group := testPBFBytesField(nil, 3, way)
	seed := testPBFBytesField(nil, 1, stringTable)
	seed = testPBFBytesField(seed, 2, group)
	f.Add(seed)

	f.Fuzz(func(t *testing.T, block []byte) {
		if len(block) > 1<<20 || validatePBFPrimitiveBlock(block, DefaultMaxWayNodes) != nil {
			return
		}
		path := writeTestPBF(t, testPBFFileBlock("OSMData", block))
		_ = ScanWays(context.Background(), path, func(Way) error { return nil })
	})
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
