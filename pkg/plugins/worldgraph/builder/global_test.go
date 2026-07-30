package builder

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

func TestBuildGlobalResumesStagesAndPublishesPackedGeneration(t *testing.T) {
	pbf := writeGlobalTestPBF(t)
	root := t.TempDir()
	storePath := filepath.Join(root, "store")
	workDir := filepath.Join(root, "work")
	options := GlobalOptions{
		PBFPath: pbf, StorePath: storePath, WorkDir: workDir,
		RunMemoryBytes: 24, PackSegmentBytes: 1_024, MaxOpenShards: 2, Resume: true,
	}
	stages := []string{
		"validate-source",
		"spool-ways-and-refs",
		"sort-refs",
		"select-nodes",
		"partition-contributions",
		"write-packs",
	}
	var stagedPackHashes map[string][sha256.Size]byte
	for _, stop := range stages {
		ctx, cancel := context.WithCancel(context.Background())
		options.Progress = func(progress GlobalProgress) {
			if progress.Stage == stop && progress.Completed {
				cancel()
			}
		}
		if _, err := BuildGlobal(ctx, options); !errors.Is(err, context.Canceled) {
			t.Fatalf("BuildGlobal(cancel after %s) error = %v, want context.Canceled", stop, err)
		}
		state, err := readGlobalBuildState(filepath.Join(workDir, globalBuildStateFilename))
		if err != nil {
			t.Fatal(err)
		}
		if !state.hasStage(stop) {
			t.Fatalf("stage %q not persisted: %+v", stop, state.Completed)
		}
		store, err := worldgraph.OpenStore(storePath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Manifest(); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("manifest visible after %s: %v", stop, err)
		}
		_ = store.Close()
		if stop == "write-packs" {
			stagedPackHashes = hashFilesWithExtension(t, state.StagePath, ".pack")
			if len(stagedPackHashes) < 3 {
				t.Fatalf("staged pack count = %d, want at least 3", len(stagedPackHashes))
			}
		}
	}

	options.Progress = nil
	manifest, err := BuildGlobal(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Layout != worldgraph.PackedLayout || manifest.Zoom != worldgraph.GlobalRoutingZoom ||
		manifest.ShardZoom != worldgraph.PackedShardZoom || len(manifest.Shards) != 3 || len(manifest.Tiles) != 0 ||
		len(manifest.Regions) != 1 || len(manifest.Regions[0].Tiles) != 0 {
		t.Fatalf("global manifest = %+v", manifest)
	}
	finalRoot := filepath.Join(storePath, "generations", manifest.Generation)
	if got := hashFilesWithExtension(t, finalRoot, ".pack"); !reflect.DeepEqual(got, stagedPackHashes) {
		t.Fatalf("final pack hashes = %v, want staged hashes %v", got, stagedPackHashes)
	}
	state, err := readGlobalBuildState(filepath.Join(workDir, globalBuildStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !state.hasStage("publish") || state.Counts.Shards != 3 || state.Counts.Chunks < 3 || state.Counts.Nodes != 6 {
		t.Fatalf("final state = %+v", state)
	}
	store, err := worldgraph.OpenStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, position := range [][2]float64{{12.56, 55.67}, {-34.90, -8.05}, {139.69, 35.68}} {
		tile, err := worldgraph.TileForPosition(position[0], position[1], worldgraph.GlobalRoutingZoom)
		if err != nil {
			t.Fatal(err)
		}
		covered, err := store.Covers(context.Background(), tile)
		if err != nil {
			t.Fatal(err)
		}
		if !covered {
			t.Fatalf("fixture tile %+v is not covered", tile)
		}
	}
}

func TestBuildGlobalVerifiesSourceAgainBeforePublication(t *testing.T) {
	pbf := writeGlobalTestPBF(t)
	data, err := os.ReadFile(pbf)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	options := GlobalOptions{
		PBFPath: pbf, StorePath: filepath.Join(root, "store"), WorkDir: filepath.Join(root, "work"),
		RunMemoryBytes: 24, PackSegmentBytes: 1_024, MaxOpenShards: 2, Resume: true,
	}
	options.Progress = func(progress GlobalProgress) {
		if progress.Stage != "write-packs" || !progress.Completed {
			return
		}
		mutated := append([]byte(nil), data...)
		mutated[len(mutated)-1] ^= 0xff
		if err := os.WriteFile(pbf, mutated, 0o600); err != nil {
			t.Fatalf("mutate PBF: %v", err)
		}
	}
	if _, err := BuildGlobal(context.Background(), options); !errors.Is(err, ErrPBFSourceChanged) {
		t.Fatalf("BuildGlobal() error = %v, want ErrPBFSourceChanged", err)
	}
	store, err := worldgraph.OpenStore(options.StorePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Manifest(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest visible after source mutation: %v", err)
	}
}

func hashFilesWithExtension(t *testing.T, root, extension string) map[string][sha256.Size]byte {
	t.Helper()
	hashes := make(map[string][sha256.Size]byte)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != extension {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		hashes[relative] = sha256.Sum256(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return hashes
}

type globalFixtureNode struct {
	id       int64
	lon, lat float64
}

type globalFixtureWay struct {
	id   int64
	from int64
	to   int64
	name string
}

func writeGlobalTestPBF(t *testing.T) string {
	t.Helper()
	nodes := []globalFixtureNode{
		{id: 1, lon: 12.56, lat: 55.67}, {id: 2, lon: 12.57, lat: 55.68},
		{id: 3, lon: -34.90, lat: -8.05}, {id: 4, lon: -34.89, lat: -8.04},
		{id: 5, lon: 139.69, lat: 35.68}, {id: 6, lon: 139.70, lat: 35.69},
	}
	ways := []globalFixtureWay{
		{id: 100, from: 1, to: 2, name: "Copenhagen"},
		{id: 101, from: 3, to: 4, name: "Recife"},
		{id: 102, from: 5, to: 6, name: "Tokyo"},
	}
	stringsTable := []string{"", "highway", "residential", "name", "Copenhagen", "Recife", "Tokyo"}
	var stringTable []byte
	for _, value := range stringsTable {
		stringTable = testPBFBytesField(stringTable, 1, []byte(value))
	}
	stringIDs := map[string]uint64{"highway": 1, "residential": 2, "name": 3, "Copenhagen": 4, "Recife": 5, "Tokyo": 6}

	var ids, lats, lons []int64
	var previousID, previousLat, previousLon int64
	for _, node := range nodes {
		lat := int64(math.Round(node.lat * 1e7))
		lon := int64(math.Round(node.lon * 1e7))
		ids = append(ids, node.id-previousID)
		lats = append(lats, lat-previousLat)
		lons = append(lons, lon-previousLon)
		previousID, previousLat, previousLon = node.id, lat, lon
	}
	var dense []byte
	dense = testPBFBytesField(dense, 1, testPBFPackedSInt64(ids...))
	dense = testPBFBytesField(dense, 8, testPBFPackedSInt64(lats...))
	dense = testPBFBytesField(dense, 9, testPBFPackedSInt64(lons...))
	nodeGroup := testPBFBytesField(nil, 2, dense)

	var wayGroup []byte
	for _, way := range ways {
		encoded := testPBFVarintField(nil, 1, uint64(way.id))
		encoded = testPBFBytesField(encoded, 2, testPBFPackedVarints(stringIDs["highway"], stringIDs["name"]))
		encoded = testPBFBytesField(encoded, 3, testPBFPackedVarints(stringIDs["residential"], stringIDs[way.name]))
		encoded = testPBFBytesField(encoded, 8, testPBFPackedSInt64(way.from, way.to-way.from))
		wayGroup = testPBFBytesField(wayGroup, 3, encoded)
	}
	block := testPBFBytesField(nil, 1, stringTable)
	block = testPBFBytesField(block, 2, nodeGroup)
	block = testPBFBytesField(block, 2, wayGroup)
	path := writeTestPBF(t, testPBFFileBlock("OSMData", block))
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}
