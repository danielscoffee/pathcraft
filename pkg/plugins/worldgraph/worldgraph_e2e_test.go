package worldgraph_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
	_ "github.com/danielscoffee/pathcraft/pkg/plugins/car"
	_ "github.com/danielscoffee/pathcraft/pkg/plugins/walk"
	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph/builder"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestWorldGraphEndToEnd(t *testing.T) {
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "world")
	fixture := filepath.Join("builder", "testdata", "seam.osm.pbf")
	manifest, err := builder.Build(ctx, builder.Options{
		PBFPath: fixture, StorePath: storePath, Region: "demo", Zoom: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Generation == "" || len(manifest.Tiles) != 2 {
		t.Fatalf("manifest = %+v", manifest)
	}

	router, err := worldgraph.OpenRouter(storePath, worldgraph.RouterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := router.Close(); err != nil {
			t.Error(err)
		}
	})
	generation, zoom, _ := router.ChunkConfig()
	if generation != manifest.Generation || zoom != 12 {
		t.Fatalf("ChunkConfig() = %q, %d", generation, zoom)
	}

	request := core.ModeRequest{
		From: core.Position{12.5683, 55.6761},
		To:   core.Position{12.5685, 55.6762},
	}
	var walkMode core.Mode
	for _, name := range []string{"walk", "car"} {
		mode, ok := plugins.Default.Mode(name)
		if !ok {
			t.Fatalf("mode %q is not registered", name)
		}
		if name == "walk" {
			walkMode = mode
		}
		t.Run(name, func(t *testing.T) {
			result, err := mode.Route(ctx, router, request)
			if err != nil {
				t.Fatal(err)
			}
			nodes, _ := result.Meta["nodes"].([]int64)
			if result.Meta["from_node_id"] != int64(1) || result.Meta["to_node_id"] != int64(3) || len(nodes) < 3 {
				t.Fatalf("route = %+v", result)
			}
		})
	}

	type renderedEdge struct{ wayID, from, to int64 }
	ownedEdges := make(map[renderedEdge]int)
	for _, tile := range manifest.Tiles {
		data, err := router.ChunkGeoJSON(ctx, tile.Z, tile.X, tile.Y)
		if err != nil {
			t.Fatal(err)
		}
		var collection struct {
			Features []struct {
				Properties struct {
					WayID int64 `json:"way_id"`
					From  int64 `json:"from"`
					To    int64 `json:"to"`
				} `json:"properties"`
			} `json:"features"`
		}
		if err := json.Unmarshal(data, &collection); err != nil {
			t.Fatal(err)
		}
		for _, feature := range collection.Features {
			ownedEdges[renderedEdge{
				wayID: feature.Properties.WayID,
				from:  feature.Properties.From,
				to:    feature.Properties.To,
			}]++
		}
	}
	if len(ownedEdges) != 6 {
		t.Fatalf("owned edge count = %d, want 6: %v", len(ownedEdges), ownedEdges)
	}
	for edge, count := range ownedEdges {
		if count != 1 {
			t.Fatalf("owned edge %+v rendered %d times", edge, count)
		}
	}

	nodesOnlyPBF := filepath.Join(t.TempDir(), "deleted.osm.pbf")
	writeNodesOnlyPBF(t, nodesOnlyPBF)
	replacement, err := builder.Build(ctx, builder.Options{
		PBFPath: nodesOnlyPBF, StorePath: storePath, Region: "demo", Zoom: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Generation == manifest.Generation || len(replacement.Tiles) != 2 {
		t.Fatalf("replacement manifest = %+v", replacement)
	}
	pinnedGeneration, _, _ := router.ChunkConfig()
	if pinnedGeneration != manifest.Generation {
		t.Fatalf("pinned router generation = %q, want %q", pinnedGeneration, manifest.Generation)
	}
	if _, err := walkMode.Route(ctx, router, request); err != nil {
		t.Fatalf("pinned generation route after replacement: %v", err)
	}

	reopened, err := worldgraph.OpenRouter(storePath, worldgraph.RouterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	reopenedGeneration, _, _ := reopened.ChunkConfig()
	if reopenedGeneration != replacement.Generation {
		t.Fatalf("reopened generation = %q, want %q", reopenedGeneration, replacement.Generation)
	}
	for _, tile := range replacement.Tiles {
		data, err := reopened.ChunkGeoJSON(ctx, tile.Z, tile.X, tile.Y)
		if err != nil {
			t.Fatal(err)
		}
		var collection struct {
			Features []json.RawMessage `json:"features"`
		}
		if err := json.Unmarshal(data, &collection); err != nil {
			t.Fatal(err)
		}
		if len(collection.Features) != 0 {
			t.Fatalf("replacement tile %+v renders %d deleted edges", tile, len(collection.Features))
		}
	}
	if _, err := walkMode.Route(ctx, reopened, request); !errors.Is(err, worldgraph.ErrNoPath) {
		t.Fatalf("route after deletion error = %v, want ErrNoPath", err)
	}
}

func writeNodesOnlyPBF(t *testing.T, path string) {
	t.Helper()
	var headerBlock []byte
	headerBlock = appendPBFString(headerBlock, 4, "OsmSchema-V0.6")
	headerBlock = appendPBFString(headerBlock, 4, "DenseNodes")
	data := appendPBFFileBlock(nil, "OSMHeader", headerBlock)

	ids := []int64{1, 1, 1}
	latitudes := []int64{pbfCoordinate(55.6761), 0, pbfCoordinate(55.6762) - pbfCoordinate(55.6761)}
	longitudes := []int64{pbfCoordinate(12.5683), pbfCoordinate(12.5684) - pbfCoordinate(12.5683), pbfCoordinate(12.5685) - pbfCoordinate(12.5684)}
	var dense []byte
	dense = appendPBFBytes(dense, 1, packedPBFSInt64(ids))
	dense = appendPBFBytes(dense, 8, packedPBFSInt64(latitudes))
	dense = appendPBFBytes(dense, 9, packedPBFSInt64(longitudes))
	group := appendPBFBytes(nil, 2, dense)
	stringTable := appendPBFBytes(nil, 1, nil)
	primitiveBlock := appendPBFBytes(nil, 1, stringTable)
	primitiveBlock = appendPBFBytes(primitiveBlock, 2, group)
	data = appendPBFFileBlock(data, "OSMData", primitiveBlock)

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendPBFFileBlock(data []byte, kind string, payload []byte) []byte {
	blob := appendPBFBytes(nil, 1, payload)
	blobHeader := appendPBFString(nil, 1, kind)
	blobHeader = protowire.AppendTag(blobHeader, 3, protowire.VarintType)
	blobHeader = protowire.AppendVarint(blobHeader, uint64(len(blob)))
	var headerLength [4]byte
	binary.BigEndian.PutUint32(headerLength[:], uint32(len(blobHeader)))
	data = append(data, headerLength[:]...)
	data = append(data, blobHeader...)
	return append(data, blob...)
}

func appendPBFBytes(data []byte, field protowire.Number, value []byte) []byte {
	data = protowire.AppendTag(data, field, protowire.BytesType)
	return protowire.AppendBytes(data, value)
}

func appendPBFString(data []byte, field protowire.Number, value string) []byte {
	data = protowire.AppendTag(data, field, protowire.BytesType)
	return protowire.AppendString(data, value)
}

func packedPBFSInt64(values []int64) []byte {
	var packed []byte
	for _, value := range values {
		packed = protowire.AppendVarint(packed, protowire.EncodeZigZag(value))
	}
	return packed
}

func pbfCoordinate(value float64) int64 {
	return int64(math.Round(value * 1e7))
}
