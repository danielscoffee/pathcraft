package builder

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	internalosm "github.com/danielscoffee/pathcraft/internal/osm"
	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

func TestFragmentSpoolCodecRoundTripIsCompactAndDerivesOwners(t *testing.T) {
	shard := worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: 17, Y: 2}
	left := fragmentTestTile(t, shard, 5, 7)
	right := fragmentTestTile(t, shard, 6, 7)
	fragment := globalWayFragment{
		WayID: 42, Highway: "residential", Name: "Compact Street",
		Direction: internalosm.DirectionReverse, RestrictWalking: true,
		Segments: []globalFragmentSegment{
			{From: fragmentTestNode(t, left, 10, 0.8, 0.4), To: fragmentTestNode(t, right, 11, 0.2, 0.4)},
			{From: fragmentTestNode(t, right, 11, 0.2, 0.4), To: fragmentTestNode(t, right, 12, 0.8, 0.6)},
		},
	}
	var encoded bytes.Buffer
	if err := writeFragmentSpoolRecord(&encoded, shard, fragment); err != nil {
		t.Fatal(err)
	}
	wantBytes := 4 + 4 + 8 + 1 + 2 + len(fragment.Highway) + 2 + len(fragment.Name) + 4 + len(fragment.Segments)*fragmentSpoolSegmentBytes
	if encoded.Len() != wantBytes {
		t.Fatalf("encoded fragment bytes = %d, want %d", encoded.Len(), wantBytes)
	}
	if bytes.Count(encoded.Bytes(), []byte(fragment.Highway)) != 1 || bytes.Count(encoded.Bytes(), []byte(fragment.Name)) != 1 {
		t.Fatal("way metadata was not stored exactly once")
	}

	decoded, ok, err := readFragmentSpoolRecord(bytes.NewReader(encoded.Bytes()), shard)
	if err != nil || !ok {
		t.Fatalf("readFragmentSpoolRecord() = %+v, %v, %v", decoded, ok, err)
	}
	if !reflect.DeepEqual(decoded, fragment) {
		t.Fatalf("decoded fragment = %+v, want %+v", decoded, fragment)
	}
	for _, segment := range decoded.Segments {
		for _, node := range []worldgraph.Node{segment.From, segment.To} {
			owner, err := worldgraph.TileForPosition(node.Lon, node.Lat, worldgraph.GlobalRoutingZoom)
			if err != nil || node.Owner != owner {
				t.Fatalf("decoded node %d owner = %+v, want %+v: %v", node.ID, node.Owner, owner, err)
			}
		}
	}
	if _, ok, err := readFragmentSpoolRecord(bytes.NewReader(nil), shard); err != nil || ok {
		t.Fatalf("empty read = %v, %v", ok, err)
	}

	var full bytes.Buffer
	for _, segment := range fragment.Segments {
		for _, contribution := range referenceFragmentSegmentContributions(t, fragment, segment) {
			actual, err := packedBuilderShard(contribution.tile)
			if err != nil {
				t.Fatal(err)
			}
			if actual == shard {
				if err := writeShardSpoolRecord(&full, contribution); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if encoded.Len() >= full.Len() {
		t.Fatalf("fragment record size = %d, full directed contributions = %d", encoded.Len(), full.Len())
	}
}

func TestFragmentSpoolCodecRejectsCorruption(t *testing.T) {
	shard := worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: 17, Y: 2}
	fragment := fragmentTestWay(t, shard, 100)
	var valid bytes.Buffer
	if err := writeFragmentSpoolRecord(&valid, shard, fragment); err != nil {
		t.Fatal(err)
	}
	original := valid.Bytes()
	policyOffset := 4 + 4 + 8
	highwayLengthOffset := policyOffset + 1
	nameLengthOffset := highwayLengthOffset + 2 + len(fragment.Highway)
	segmentCountOffset := nameLengthOffset + 2 + len(fragment.Name)
	firstNodeOffset := segmentCountOffset + 4

	mutate := func(change func([]byte)) []byte {
		data := append([]byte(nil), original...)
		change(data)
		return data
	}
	withTrailingPayload := append([]byte(nil), original...)
	binary.BigEndian.PutUint32(withTrailingPayload[:4], binary.BigEndian.Uint32(withTrailingPayload[:4])+1)
	withTrailingPayload = append(withTrailingPayload, 0xff)

	tests := []struct {
		name string
		data []byte
	}{
		{name: "truncated length", data: original[:3]},
		{name: "truncated payload", data: original[:len(original)-1]},
		{name: "short length", data: []byte{0, 0, 0, 1, 0}},
		{name: "oversized length", data: func() []byte {
			data := make([]byte, 4)
			binary.BigEndian.PutUint32(data, uint32(maxFragmentSpoolRecordBytes+1))
			return data
		}()},
		{name: "unsupported version", data: mutate(func(data []byte) { binary.BigEndian.PutUint32(data[4:8], fragmentSpoolVersion+1) })},
		{name: "invalid policy direction", data: mutate(func(data []byte) { data[policyOffset] = 3 })},
		{name: "invalid policy bits", data: mutate(func(data []byte) { data[policyOffset] = 0x80 })},
		{name: "oversized highway", data: mutate(func(data []byte) {
			binary.BigEndian.PutUint16(data[highwayLengthOffset:], worldgraph.MaxEdgeHighwayBytes+1)
		})},
		{name: "zero segments", data: mutate(func(data []byte) { binary.BigEndian.PutUint32(data[segmentCountOffset:], 0) })},
		{name: "nan longitude", data: mutate(func(data []byte) { binary.BigEndian.PutUint64(data[firstNodeOffset+8:], math.Float64bits(math.NaN())) })},
		{name: "outside mercator", data: mutate(func(data []byte) { binary.BigEndian.PutUint64(data[firstNodeOffset+16:], math.Float64bits(90)) })},
		{name: "repeated endpoint", data: mutate(func(data []byte) {
			copy(data[firstNodeOffset+24:firstNodeOffset+32], data[firstNodeOffset:firstNodeOffset+8])
		})},
		{name: "trailing payload", data: withTrailingPayload},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := readFragmentSpoolRecord(bytes.NewReader(test.data), shard); !errors.Is(err, ErrCorruptFragmentSpool) {
				t.Fatalf("readFragmentSpoolRecord() error = %v, want ErrCorruptFragmentSpool", err)
			}
		})
	}

	wrongShard := shard
	wrongShard.X++
	if _, _, err := readFragmentSpoolRecord(bytes.NewReader(original), wrongShard); !errors.Is(err, ErrCorruptFragmentSpool) {
		t.Fatalf("wrong-shard read error = %v, want ErrCorruptFragmentSpool", err)
	}
}

func TestFragmentSpoolValidationAndLimits(t *testing.T) {
	shard := worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: 17, Y: 2}
	valid := fragmentTestWay(t, shard, 200)
	tests := []struct {
		name   string
		change func(*globalWayFragment)
	}{
		{name: "empty highway", change: func(fragment *globalWayFragment) { fragment.Highway = "" }},
		{name: "long highway", change: func(fragment *globalWayFragment) {
			fragment.Highway = strings.Repeat("h", worldgraph.MaxEdgeHighwayBytes+1)
		}},
		{name: "long name", change: func(fragment *globalWayFragment) { fragment.Name = strings.Repeat("n", worldgraph.MaxEdgeNameBytes+1) }},
		{name: "invalid direction", change: func(fragment *globalWayFragment) { fragment.Direction = internalosm.Direction(99) }},
		{name: "empty segments", change: func(fragment *globalWayFragment) { fragment.Segments = nil }},
		{name: "too many segments", change: func(fragment *globalWayFragment) {
			fragment.Segments = make([]globalFragmentSegment, maxFragmentSegments+1)
		}},
		{name: "same endpoint", change: func(fragment *globalWayFragment) { fragment.Segments[0].To = fragment.Segments[0].From }},
		{name: "wrong owner", change: func(fragment *globalWayFragment) { fragment.Segments[0].From.Owner.X++ }},
		{name: "nan longitude", change: func(fragment *globalWayFragment) { fragment.Segments[0].From.Lon = math.NaN() }},
		{name: "longitude outside osm", change: func(fragment *globalWayFragment) { fragment.Segments[0].From.Lon = 181 }},
		{name: "latitude outside mercator", change: func(fragment *globalWayFragment) {
			fragment.Segments[0].From.Lat = worldgraph.MaxMercatorLatitude + 0.001
		}},
		{name: "conflicting node", change: func(fragment *globalWayFragment) {
			other := fragment.Segments[0]
			other.From.Lon += 0.000001
			other.From.Owner, _ = worldgraph.TileForPosition(other.From.Lon, other.From.Lat, worldgraph.GlobalRoutingZoom)
			fragment.Segments = append(fragment.Segments, other)
		}},
		{name: "another shard", change: func(fragment *globalWayFragment) {
			other := worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: shard.X + 2, Y: shard.Y}
			tile := fragmentTestTile(t, other, 0, 0)
			fragment.Segments[0] = globalFragmentSegment{
				From: fragmentTestNode(t, tile, 1, 0.2, 0.4), To: fragmentTestNode(t, tile, 2, 0.8, 0.6),
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fragment := cloneGlobalWayFragment(valid)
			test.change(&fragment)
			if err := writeFragmentSpoolRecord(&bytes.Buffer{}, shard, fragment); err == nil {
				t.Fatal("writeFragmentSpoolRecord() error = nil")
			}
		})
	}
	if err := writeFragmentSpoolRecord(nil, shard, valid); err == nil {
		t.Fatal("nil codec writer error = nil")
	}
	if _, _, err := readFragmentSpoolRecord(nil, shard); err == nil {
		t.Fatal("nil codec reader error = nil")
	}
}

func TestFragmentSpoolWriterBoundsLRUFlushesAndReopens(t *testing.T) {
	root := t.TempDir()
	writer, err := newFragmentSpoolWriter(context.Background(), root, 2)
	if err != nil {
		t.Fatal(err)
	}
	shards := []worldgraph.TileID{
		{Z: worldgraph.PackedShardZoom, X: 19, Y: 2},
		{Z: worldgraph.PackedShardZoom, X: 17, Y: 2},
		{Z: worldgraph.PackedShardZoom, X: 18, Y: 2},
	}
	fragments := make([]globalWayFragment, len(shards))
	for index, shard := range shards {
		fragments[index] = fragmentTestWay(t, shard, int64(300+index*10))
		if err := writer.Add(shard, fragments[index]); err != nil {
			t.Fatal(err)
		}
		if len(writer.open) > 2 || writer.lru.Len() > 2 {
			t.Fatalf("open descriptors = %d/%d, limit 2", len(writer.open), writer.lru.Len())
		}
	}
	firstPath, err := fragmentSpoolPath(root, shards[0])
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(firstPath); err != nil || info.Size() == 0 {
		t.Fatalf("evicted spool was not flushed: %v, %+v", err, info)
	}
	if err := writer.Add(shards[0], fragments[0]); err != nil {
		t.Fatal(err)
	}
	if len(writer.open) > 2 {
		t.Fatalf("open descriptors after reopen = %d", len(writer.open))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if len(writer.open) != 0 || writer.lru.Len() != 0 {
		t.Fatalf("writer retained descriptors after close: %d/%d", len(writer.open), writer.lru.Len())
	}
	if err := writer.Add(shards[0], fragments[0]); err == nil {
		t.Fatal("Add() after Close() error = nil")
	}

	wantShards := append([]worldgraph.TileID(nil), shards...)
	sort.Slice(wantShards, func(i, j int) bool {
		if wantShards[i].X != wantShards[j].X {
			return wantShards[i].X < wantShards[j].X
		}
		return wantShards[i].Y < wantShards[j].Y
	})
	if got := writer.Shards(); !reflect.DeepEqual(got, wantShards) {
		t.Fatalf("Shards() = %+v, want %+v", got, wantShards)
	}
	wantPath := filepath.Join(root, "fragments", "1", "19", "2.spool")
	if firstPath != wantPath {
		t.Fatalf("fragmentSpoolPath() = %q, want %q", firstPath, wantPath)
	}
	for _, shard := range shards {
		path, err := fragmentSpoolPath(root, shard)
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

	file, err := os.Open(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for index := 0; index < 2; index++ {
		got, ok, err := readFragmentSpoolRecord(file, shards[0])
		if err != nil || !ok || !reflect.DeepEqual(got, fragments[0]) {
			t.Fatalf("reopened record %d = %+v, %v, %v", index, got, ok, err)
		}
	}
	if _, ok, err := readFragmentSpoolRecord(file, shards[0]); err != nil || ok {
		t.Fatalf("record after reopened records = %v, %v", ok, err)
	}
}

func TestReplayFragmentSpoolMatchesSharedExpansionAcrossSeams(t *testing.T) {
	equatorY := 1 << (worldgraph.GlobalRoutingZoom - 1)
	sameShard := worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: 17, Y: equatorY >> (worldgraph.GlobalRoutingZoom - worldgraph.PackedShardZoom)}
	sameTile := worldgraph.TileID{Z: worldgraph.GlobalRoutingZoom, X: sameShard.X << 4, Y: equatorY}
	z12Right := sameTile
	z12Right.X++
	shardLeftTile := sameTile
	shardLeftTile.X = (sameShard.X+1)<<4 - 1
	shardRightTile := shardLeftTile
	shardRightTile.X++
	antimeridianWest := worldgraph.TileID{Z: worldgraph.GlobalRoutingZoom, X: (1 << worldgraph.GlobalRoutingZoom) - 1, Y: equatorY}
	antimeridianEast := worldgraph.TileID{Z: worldgraph.GlobalRoutingZoom, X: 0, Y: equatorY}

	tests := []struct {
		name     string
		from, to worldgraph.Node
	}{
		{name: "same tile", from: fragmentTestNode(t, sameTile, 1, 0.2, 0.4), to: fragmentTestNode(t, sameTile, 2, 0.8, 0.6)},
		{name: "z12 seam", from: fragmentTestNode(t, sameTile, 3, 0.9, 0.5), to: fragmentTestNode(t, z12Right, 4, 0.1, 0.5)},
		{name: "z8 shard seam", from: fragmentTestNode(t, shardLeftTile, 5, 0.9, 0.5), to: fragmentTestNode(t, shardRightTile, 6, 0.1, 0.5)},
		{name: "antimeridian", from: fragmentTestNode(t, antimeridianWest, 7, 0.9, 0.5), to: fragmentTestNode(t, antimeridianEast, 8, 0.1, 0.5)},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fragment := globalWayFragment{
				WayID: int64(400 + index), Highway: "primary", Name: test.name,
				Direction: internalosm.DirectionForward, RestrictWalking: true,
				Segments: []globalFragmentSegment{{From: test.from, To: test.to}},
			}
			expectedByShard := referenceContributionsByShard(t, fragment)
			root := t.TempDir()
			writer, err := newFragmentSpoolWriter(context.Background(), root, 1)
			if err != nil {
				t.Fatal(err)
			}
			for shard := range expectedByShard {
				if err := writer.Add(shard, fragment); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			for shard, expected := range expectedByShard {
				path, err := fragmentSpoolPath(root, shard)
				if err != nil {
					t.Fatal(err)
				}
				var got []edgeContribution
				if err := replayFragmentSpool(context.Background(), path, shard, func(contribution edgeContribution) error {
					got = append(got, contribution)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, expected) {
					t.Fatalf("shard %+v replay = %+v, want %+v", shard, got, expected)
				}
				for _, contribution := range got {
					if !reflect.DeepEqual(contribution.edge.Sources, []string{fragmentPlanetSource}) {
						t.Fatalf("edge sources = %v", contribution.edge.Sources)
					}
					if !contribution.edge.RestrictWalking {
						t.Fatal("walking restriction was lost")
					}
				}
			}
		})
	}
}

func TestReplayFragmentSpoolPreservesDirectionAndAccessFlags(t *testing.T) {
	shard := worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: 17, Y: 128}
	tests := []struct {
		name               string
		direction          internalosm.Direction
		restrictWalking    bool
		restrictDriving    bool
		wantForwardDriving bool
		wantReverseDriving bool
	}{
		{name: "both", direction: internalosm.DirectionBoth},
		{name: "forward", direction: internalosm.DirectionForward, restrictWalking: true, wantReverseDriving: true},
		{name: "reverse", direction: internalosm.DirectionReverse, wantForwardDriving: true},
		{name: "base driving restriction", direction: internalosm.DirectionForward, restrictDriving: true, wantForwardDriving: true, wantReverseDriving: true},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fragment := fragmentTestWay(t, shard, int64(500+index*10))
			fragment.Direction = test.direction
			fragment.RestrictWalking = test.restrictWalking
			fragment.RestrictDriving = test.restrictDriving
			path := writeFragmentTestFile(t, shard, fragment)
			var contributions []edgeContribution
			if err := replayFragmentSpool(context.Background(), path, shard, func(contribution edgeContribution) error {
				contributions = append(contributions, contribution)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if len(contributions) != 2 {
				t.Fatalf("contribution count = %d, want 2", len(contributions))
			}
			forward, reverse := contributions[0].edge, contributions[1].edge
			if forward.ID.From != fragment.Segments[0].From.ID || reverse.ID.From != fragment.Segments[0].To.ID {
				t.Fatalf("edge orientation = %+v, %+v", forward.ID, reverse.ID)
			}
			if forward.RestrictDriving != test.wantForwardDriving || reverse.RestrictDriving != test.wantReverseDriving {
				t.Fatalf("driving flags = %v/%v, want %v/%v", forward.RestrictDriving, reverse.RestrictDriving, test.wantForwardDriving, test.wantReverseDriving)
			}
			if forward.RestrictWalking != test.restrictWalking || reverse.RestrictWalking != test.restrictWalking {
				t.Fatalf("walking flags = %v/%v, want %v", forward.RestrictWalking, reverse.RestrictWalking, test.restrictWalking)
			}
			if forward.DistanceMeters != reverse.DistanceMeters || forward.Owner != reverse.Owner || !reflect.DeepEqual(forward.Sources, []string{"planet"}) || !reflect.DeepEqual(reverse.Sources, []string{"planet"}) {
				t.Fatalf("edge derivation differs: %+v / %+v", forward, reverse)
			}
		})
	}
}

func TestFragmentSpoolAcceptsMercatorLimitAndRejectsPolarOverflow(t *testing.T) {
	from := fragmentNodeForPosition(t, 600, 0, worldgraph.MaxMercatorLatitude)
	to := fragmentNodeForPosition(t, 601, 0.001, worldgraph.MaxMercatorLatitude-0.000001)
	shard, err := packedBuilderShard(from.Owner)
	if err != nil {
		t.Fatal(err)
	}
	fragment := globalWayFragment{WayID: 600, Highway: "path", Segments: []globalFragmentSegment{{From: from, To: to}}}
	if err := writeFragmentSpoolRecord(&bytes.Buffer{}, shard, fragment); err != nil {
		t.Fatalf("Mercator-limit fragment rejected: %v", err)
	}
	fragment.Segments[0].From.Lat = math.Nextafter(worldgraph.MaxMercatorLatitude, math.Inf(1))
	if err := writeFragmentSpoolRecord(&bytes.Buffer{}, shard, fragment); err == nil {
		t.Fatal("polar-overflow fragment accepted")
	}
}

func TestFragmentSpoolCancellation(t *testing.T) {
	shard := worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: 17, Y: 128}
	fragment := fragmentTestWay(t, shard, 700)
	ctx, cancel := context.WithCancel(context.Background())
	writer, err := newFragmentSpoolWriter(ctx, t.TempDir(), 1)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := writer.Add(shard, fragment); !errors.Is(err, context.Canceled) {
		t.Fatalf("Add() error = %v, want context.Canceled", err)
	}
	if err := writer.Close(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close() error = %v, want context.Canceled", err)
	}
	if _, err := newFragmentSpoolWriter(ctx, t.TempDir(), 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("newFragmentSpoolWriter() error = %v, want context.Canceled", err)
	}

	path := writeFragmentTestFile(t, shard, fragment)
	replayCtx, replayCancel := context.WithCancel(context.Background())
	consumed := 0
	err = replayFragmentSpool(replayCtx, path, shard, func(edgeContribution) error {
		consumed++
		replayCancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || consumed != 1 {
		t.Fatalf("cancelled replay = %v after %d contributions", err, consumed)
	}
	if err := replayFragmentSpool(nil, path, shard, func(edgeContribution) error { return nil }); err == nil {
		t.Fatal("nil replay context error = nil")
	}
	if err := replayFragmentSpool(context.Background(), path, shard, nil); err == nil {
		t.Fatal("nil replay consumer error = nil")
	}
	buildCtx, buildCancel := context.WithCancel(context.Background())
	buildCancel()
	if _, _, err := buildPackedShardFromFragmentSpool(buildCtx, path, shard, t.TempDir(), 1024); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled build error = %v, want context.Canceled", err)
	}
}

func TestFragmentSpoolDifferentialChunksAndPackedOutput(t *testing.T) {
	equatorY := 1 << (worldgraph.GlobalRoutingZoom - 1)
	shardA := worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: 17, Y: 128}
	tileA := worldgraph.TileID{Z: worldgraph.GlobalRoutingZoom, X: shardA.X << 4, Y: equatorY}
	tileANext := tileA
	tileANext.X++
	shardEdgeLeft := tileA
	shardEdgeLeft.X = ((shardA.X + 1) << 4) - 1
	shardEdgeRight := shardEdgeLeft
	shardEdgeRight.X++
	antiWest := worldgraph.TileID{Z: worldgraph.GlobalRoutingZoom, X: (1 << worldgraph.GlobalRoutingZoom) - 1, Y: equatorY}
	antiEast := worldgraph.TileID{Z: worldgraph.GlobalRoutingZoom, X: 0, Y: equatorY}

	ways := []globalWayFragment{
		{
			WayID: 800, Highway: "residential", Name: "Local",
			Segments: []globalFragmentSegment{
				{From: fragmentTestNode(t, tileA, 8001, 0.2, 0.4), To: fragmentTestNode(t, tileA, 8002, 0.8, 0.5)},
				{From: fragmentTestNode(t, tileA, 8002, 0.8, 0.5), To: fragmentTestNode(t, tileANext, 8003, 0.2, 0.6)},
			},
		},
		{
			WayID: 801, Highway: "primary", Name: "Shard Seam", Direction: internalosm.DirectionForward, RestrictWalking: true,
			Segments: []globalFragmentSegment{{From: fragmentTestNode(t, shardEdgeLeft, 8011, 0.9, 0.5), To: fragmentTestNode(t, shardEdgeRight, 8012, 0.1, 0.5)}},
		},
		{
			WayID: 802, Highway: "trunk", Name: "Date Line", Direction: internalosm.DirectionReverse, RestrictWalking: true,
			Segments: []globalFragmentSegment{{From: fragmentTestNode(t, antiWest, 8021, 0.9, 0.5), To: fragmentTestNode(t, antiEast, 8022, 0.1, 0.5)}},
		},
	}

	oldRoot := filepath.Join(t.TempDir(), "full")
	newRoot := filepath.Join(t.TempDir(), "fragments")
	oldWriter, err := newShardSpoolWriter(context.Background(), oldRoot, 2)
	if err != nil {
		t.Fatal(err)
	}
	newWriter, err := newFragmentSpoolWriter(context.Background(), newRoot, 2)
	if err != nil {
		t.Fatal(err)
	}
	referenceByShard := make(map[worldgraph.TileID][]edgeContribution)
	for _, way := range ways {
		segmentsByShard := make(map[worldgraph.TileID][]globalFragmentSegment)
		for _, segment := range way.Segments {
			seenSegmentShard := make(map[worldgraph.TileID]struct{})
			for _, contribution := range referenceFragmentSegmentContributions(t, way, segment) {
				if err := oldWriter.Add(contribution); err != nil {
					t.Fatal(err)
				}
				shard, err := packedBuilderShard(contribution.tile)
				if err != nil {
					t.Fatal(err)
				}
				referenceByShard[shard] = append(referenceByShard[shard], contribution)
				if _, exists := seenSegmentShard[shard]; !exists {
					segmentsByShard[shard] = append(segmentsByShard[shard], segment)
					seenSegmentShard[shard] = struct{}{}
				}
			}
		}
		for shard, segments := range segmentsByShard {
			fragment := way
			fragment.Segments = append([]globalFragmentSegment(nil), segments...)
			if err := newWriter.Add(shard, fragment); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := oldWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := newWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(newWriter.Shards(), oldWriter.Shards()) {
		t.Fatalf("fragment shards = %+v, full shards = %+v", newWriter.Shards(), oldWriter.Shards())
	}

	oldStage := filepath.Join(t.TempDir(), "old-stage")
	newStage := filepath.Join(t.TempDir(), "new-stage")
	if err := os.MkdirAll(oldStage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newStage, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, shard := range newWriter.Shards() {
		fragmentPath, err := fragmentSpoolPath(newRoot, shard)
		if err != nil {
			t.Fatal(err)
		}
		var replayed []edgeContribution
		if err := replayFragmentSpool(context.Background(), fragmentPath, shard, func(contribution edgeContribution) error {
			replayed = append(replayed, contribution)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(normalizedTestChunks(t, replayed), normalizedTestChunks(t, referenceByShard[shard])) {
			t.Fatalf("normalized chunks differ for shard %+v", shard)
		}

		fullPath, err := shardSpoolPath(oldRoot, shard)
		if err != nil {
			t.Fatal(err)
		}
		oldChunks, oldEdges, err := buildPackedShardFromSpool(context.Background(), fullPath, shard, oldStage, 1024)
		if err != nil {
			t.Fatal(err)
		}
		newChunks, newEdges, err := buildPackedShardFromFragmentSpool(context.Background(), fragmentPath, shard, newStage, 1024)
		if err != nil {
			t.Fatal(err)
		}
		if oldChunks != newChunks || oldEdges != newEdges {
			t.Fatalf("shard %+v counts full=%d/%d fragment=%d/%d", shard, oldChunks, oldEdges, newChunks, newEdges)
		}
	}
	if oldFiles, newFiles := readTestFileTree(t, oldStage), readTestFileTree(t, newStage); !reflect.DeepEqual(oldFiles, newFiles) {
		t.Fatalf("packed output differs\nfull: %v\nfragment: %v", fileTreeSizes(oldFiles), fileTreeSizes(newFiles))
	}
}

func fragmentTestWay(t *testing.T, shard worldgraph.TileID, wayID int64) globalWayFragment {
	t.Helper()
	tile := fragmentTestTile(t, shard, 2, 3)
	return globalWayFragment{
		WayID: wayID, Highway: "residential", Name: "Fragment Street",
		Segments: []globalFragmentSegment{{
			From: fragmentTestNode(t, tile, wayID+1, 0.2, 0.4),
			To:   fragmentTestNode(t, tile, wayID+2, 0.8, 0.6),
		}},
	}
}

func fragmentTestTile(t *testing.T, shard worldgraph.TileID, localX, localY int) worldgraph.TileID {
	t.Helper()
	if err := validatePackedBuilderShard(shard); err != nil {
		t.Fatal(err)
	}
	width := 1 << (worldgraph.GlobalRoutingZoom - worldgraph.PackedShardZoom)
	if localX < 0 || localX >= width || localY < 0 || localY >= width {
		t.Fatalf("invalid local tile %d,%d", localX, localY)
	}
	return worldgraph.TileID{Z: worldgraph.GlobalRoutingZoom, X: shard.X*width + localX, Y: shard.Y*width + localY}
}

func fragmentTestNode(t *testing.T, tile worldgraph.TileID, id int64, xFraction, yFraction float64) worldgraph.Node {
	t.Helper()
	bounds := tile.Bounds()
	return fragmentNodeForPosition(t, id,
		bounds.West+(bounds.East-bounds.West)*xFraction,
		bounds.South+(bounds.North-bounds.South)*yFraction,
	)
}

func fragmentNodeForPosition(t *testing.T, id int64, lon, lat float64) worldgraph.Node {
	t.Helper()
	owner, err := worldgraph.TileForPosition(lon, lat, worldgraph.GlobalRoutingZoom)
	if err != nil {
		t.Fatal(err)
	}
	return worldgraph.Node{ID: id, Lon: lon, Lat: lat, Owner: owner}
}

func cloneGlobalWayFragment(fragment globalWayFragment) globalWayFragment {
	fragment.Segments = append([]globalFragmentSegment(nil), fragment.Segments...)
	return fragment
}

func writeFragmentTestFile(t *testing.T, shard worldgraph.TileID, fragments ...globalWayFragment) string {
	t.Helper()
	root := t.TempDir()
	path, err := fragmentSpoolPath(root, shard)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range fragments {
		if err := writeFragmentSpoolRecord(file, shard, fragment); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func referenceFragmentSegmentContributions(t *testing.T, fragment globalWayFragment, segment globalFragmentSegment) []edgeContribution {
	t.Helper()
	way := Way{ID: fragment.WayID, Tags: map[string]string{"highway": fragment.Highway, "name": fragment.Name}}
	policy := internalosm.WayPolicy{
		Routable: true, Direction: fragment.Direction,
		RestrictWalking: fragment.RestrictWalking, RestrictDriving: fragment.RestrictDriving,
	}
	edges, err := segmentEdges(way, policy, segment.From, segment.To, "planet", worldgraph.GlobalRoutingZoom)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Fatalf("segmentEdges() returned %d edges", len(edges))
	}
	var contributions []edgeContribution
	for _, edge := range edges {
		from, to := segment.From, segment.To
		if edge.ID.From == segment.To.ID {
			from, to = to, from
		}
		contributions = append(contributions, edgeContributions(from, to, edge)...)
	}
	return contributions
}

func referenceContributionsByShard(t *testing.T, fragment globalWayFragment) map[worldgraph.TileID][]edgeContribution {
	t.Helper()
	result := make(map[worldgraph.TileID][]edgeContribution)
	for _, segment := range fragment.Segments {
		for _, contribution := range referenceFragmentSegmentContributions(t, fragment, segment) {
			shard, err := packedBuilderShard(contribution.tile)
			if err != nil {
				t.Fatal(err)
			}
			result[shard] = append(result[shard], contribution)
		}
	}
	return result
}

func normalizedTestChunks(t *testing.T, contributions []edgeContribution) []worldgraph.Chunk {
	t.Helper()
	store, err := openContributionStore(filepath.Join(t.TempDir(), "contributions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.PutBatch(context.Background(), contributions); err != nil {
		t.Fatal(err)
	}
	tiles, err := store.Tiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(tiles, func(i, j int) bool {
		if tiles[i].X != tiles[j].X {
			return tiles[i].X < tiles[j].X
		}
		return tiles[i].Y < tiles[j].Y
	})
	chunks := make([]worldgraph.Chunk, 0, len(tiles))
	for _, tile := range tiles {
		chunk, found, err := store.Chunk(context.Background(), tile)
		if err != nil || !found {
			t.Fatalf("Chunk(%+v) = %v, %v", tile, found, err)
		}
		chunks = append(chunks, normalizeChunk(chunk))
	}
	return chunks
}

func readTestFileTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[relative], err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func fileTreeSizes(files map[string][]byte) map[string]int {
	sizes := make(map[string]int, len(files))
	for path, data := range files {
		sizes[path] = len(data)
	}
	return sizes
}
