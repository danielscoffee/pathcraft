package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

func TestShardSpoolBoundsOpenFilesAndReopensAppend(t *testing.T) {
	root := t.TempDir()
	writer, err := newShardSpoolWriter(context.Background(), root, 2)
	if err != nil {
		t.Fatal(err)
	}
	shards := []worldgraph.TileID{
		{Z: worldgraph.PackedShardZoom, X: 17, Y: 2},
		{Z: worldgraph.PackedShardZoom, X: 18, Y: 2},
		{Z: worldgraph.PackedShardZoom, X: 19, Y: 2},
	}
	first := shardSpoolTestContribution(t, shards[0], 0, 1)
	contributions := []edgeContribution{
		first,
		shardSpoolTestContribution(t, shards[1], 0, 10),
		shardSpoolTestContribution(t, shards[2], 0, 20),
		first,
	}
	for _, contribution := range contributions {
		if err := writer.Add(contribution); err != nil {
			t.Fatal(err)
		}
		if len(writer.open) > 2 {
			t.Fatalf("open shard files = %d, limit 2", len(writer.open))
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if got := writer.Shards(); !reflect.DeepEqual(got, shards) {
		t.Fatalf("Shards() = %+v, want %+v", got, shards)
	}

	path, err := shardSpoolPath(root, shards[0])
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(root, "shards", "1", "17", "2.spool")
	if path != wantPath {
		t.Fatalf("shardSpoolPath() = %q, want %q", path, wantPath)
	}
	var replayed []edgeContribution
	if err := replayShardSpool(context.Background(), path, shards[0], func(contribution edgeContribution) error {
		replayed = append(replayed, contribution)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replayed, []edgeContribution{first, first}) {
		t.Fatalf("replayed contributions = %+v", replayed)
	}
	for _, shard := range shards {
		path, err := shardSpoolPath(root, shard)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
}

func TestShardSpoolHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	writer, err := newShardSpoolWriter(ctx, t.TempDir(), 1)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	contribution := shardSpoolTestContribution(t, worldgraph.TileID{Z: worldgraph.PackedShardZoom}, 0, 1)
	if err := writer.Add(contribution); !errors.Is(err, context.Canceled) {
		t.Fatalf("Add() error = %v, want context.Canceled", err)
	}
	if err := writer.Close(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close() error = %v, want context.Canceled", err)
	}
}

func TestShardSpoolRejectsMalformedReplay(t *testing.T) {
	root := t.TempDir()
	shard := worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: 17, Y: 2}
	path, err := shardSpoolPath(root, shard)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte{0, 0, 0, 10, 1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replayShardSpool(context.Background(), path, shard, func(edgeContribution) error { return nil }); !errors.Is(err, ErrCorruptShardSpool) {
		t.Fatalf("replayShardSpool() error = %v, want ErrCorruptShardSpool", err)
	}
}

func TestGlobalContributionPartitionsAndWritesPackedShard(t *testing.T) {
	shard := worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: 17, Y: 2}
	left, err := packedBuilderTile(shard, 0)
	if err != nil {
		t.Fatal(err)
	}
	right, err := packedBuilderTile(shard, 1)
	if err != nil {
		t.Fatal(err)
	}
	leftBounds, rightBounds := left.Bounds(), right.Bounds()
	from := worldgraph.Node{ID: 1, Lon: (leftBounds.West + 9*leftBounds.East) / 10, Lat: (leftBounds.South + leftBounds.North) / 2, Owner: left}
	to := worldgraph.Node{ID: 2, Lon: (9*rightBounds.West + rightBounds.East) / 10, Lat: (rightBounds.South + rightBounds.North) / 2, Owner: right}

	root := t.TempDir()
	nodePath := filepath.Join(root, "nodes.idx")
	writeGlobalNodeTestFile(t, nodePath, []globalNodeRecord{
		{ID: from.ID, Lon: from.Lon, Lat: from.Lat, Owner: from.Owner},
		{ID: to.ID, Lon: to.Lon, Lat: to.Lat, Owner: to.Owner},
	})
	nodes, err := openGlobalNodeIndex(nodePath)
	if err != nil {
		t.Fatal(err)
	}
	defer nodes.Close()
	wayPath := filepath.Join(root, "ways.spool")
	wayFile, err := os.OpenFile(wayPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	way := Way{ID: 100, NodeIDs: []int64{from.ID, to.ID}, Tags: map[string]string{"highway": "residential", "name": "Shard Street"}}
	if err := writeWaySpoolRecord(wayFile, way, DefaultMaxWayNodes); err != nil {
		t.Fatal(err)
	}
	if err := wayFile.Close(); err != nil {
		t.Fatal(err)
	}
	spoolRoot := filepath.Join(root, "contributions")
	shards, count, err := partitionGlobalContributions(context.Background(), wayPath, nodes, spoolRoot, "planet", 1, DefaultMaxWayNodes)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(shards, []worldgraph.TileID{shard}) || count == 0 {
		t.Fatalf("partition result = %+v, %d", shards, count)
	}
	spoolPath, err := shardSpoolPath(spoolRoot, shard)
	if err != nil {
		t.Fatal(err)
	}
	if err := replayShardSpool(context.Background(), spoolPath, shard, func(contribution edgeContribution) error {
		actual, err := packedBuilderShard(contribution.tile)
		if err != nil {
			return err
		}
		if actual != shard {
			t.Fatalf("contribution tile %+v escaped shard %+v", contribution.tile, shard)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	storeRoot := filepath.Join(root, "store")
	store, err := worldgraph.OpenStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manifest := worldgraph.Manifest{
		Generation: "global-test", Zoom: worldgraph.GlobalRoutingZoom,
		Layout: worldgraph.PackedLayout, LayoutVersion: worldgraph.PackedLayoutVersion,
		ShardZoom: worldgraph.PackedShardZoom, Shards: shards, SourceBytes: 1234,
		BuiltAt: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
		Regions: []worldgraph.RegionManifest{{Name: "planet", SourceSHA256: strings.Repeat("a", 64)}},
	}
	stage, err := store.BeginPackedGeneration(manifest)
	if err != nil {
		t.Fatal(err)
	}
	chunks, edges, err := buildPackedShardFromSpool(context.Background(), spoolPath, shard, stage.Path(), 1_024)
	if err != nil {
		t.Fatal(err)
	}
	if chunks != 2 || edges != 2 {
		t.Fatalf("packed counts = %d chunks, %d edges; want 2 and 2", chunks, edges)
	}
	segments, err := filepath.Glob(filepath.Join(stage.Path(), "shards", "1", "17", "2-*.pack"))
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 {
		t.Fatalf("pack segment count = %d, want 2", len(segments))
	}
	if err := stage.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, tile := range []worldgraph.TileID{left, right} {
		chunk, err := store.LoadChunk(tile)
		if err != nil {
			t.Fatal(err)
		}
		if len(chunk.Edges) != 2 {
			t.Fatalf("tile %+v edge count = %d, want 2", tile, len(chunk.Edges))
		}
	}
}

func TestGlobalContributionRejectsMissingNode(t *testing.T) {
	root := t.TempDir()
	nodePath := filepath.Join(root, "nodes.idx")
	writeGlobalNodeTestFile(t, nodePath, []globalNodeRecord{globalNodeTestRecord(t, 1, 0, 0)})
	nodes, err := openGlobalNodeIndex(nodePath)
	if err != nil {
		t.Fatal(err)
	}
	defer nodes.Close()
	wayPath := filepath.Join(root, "ways.spool")
	wayFile, err := os.OpenFile(wayPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeWaySpoolRecord(wayFile, Way{ID: 1, NodeIDs: []int64{1, 2}, Tags: map[string]string{"highway": "residential"}}, DefaultMaxWayNodes); err != nil {
		t.Fatal(err)
	}
	if err := wayFile.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := partitionGlobalContributions(context.Background(), wayPath, nodes, filepath.Join(root, "spools"), "planet", 1, DefaultMaxWayNodes); err == nil {
		t.Fatal("partitionGlobalContributions() error = nil")
	}
}

func shardSpoolTestContribution(t *testing.T, shard worldgraph.TileID, slot uint8, id int64) edgeContribution {
	t.Helper()
	tile, err := packedBuilderTile(shard, slot)
	if err != nil {
		t.Fatal(err)
	}
	bounds := tile.Bounds()
	from := worldgraph.Node{
		ID: id, Lon: (2*bounds.West + bounds.East) / 3, Lat: (2*bounds.South + bounds.North) / 3, Owner: tile,
	}
	to := worldgraph.Node{
		ID: id + 1, Lon: (bounds.West + 2*bounds.East) / 3, Lat: (bounds.South + 2*bounds.North) / 3, Owner: tile,
	}
	owner, err := worldgraph.TileForEdge(from, to, worldgraph.GlobalRoutingZoom)
	if err != nil {
		t.Fatal(err)
	}
	edge := worldgraph.Edge{
		ID: worldgraph.EdgeID{WayID: id, From: from.ID, To: to.ID}, DistanceMeters: 100,
		Highway: "residential", Name: "Main Street", Owner: owner, Sources: []string{"planet"},
	}
	return edgeContribution{tile: tile, from: from, to: to, edge: edge}
}

func packedBuilderTile(shard worldgraph.TileID, slot uint8) (worldgraph.TileID, error) {
	width := 1 << (worldgraph.GlobalRoutingZoom - worldgraph.PackedShardZoom)
	return worldgraph.TileID{
		Z: worldgraph.GlobalRoutingZoom,
		X: shard.X*width + int(slot)%width,
		Y: shard.Y*width + int(slot)/width,
	}, nil
}
