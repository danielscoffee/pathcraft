package worldgraph_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	pathhttp "github.com/danielscoffee/pathcraft/internal/http"
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

func TestPackedWorldGraphHTTPMultiRegionEndToEnd(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	pbf := filepath.Join(root, "global.osm.pbf")
	writeGlobalE2EPBF(t, pbf)
	storePath := filepath.Join(root, "store")
	manifest, err := builder.BuildGlobal(ctx, builder.GlobalOptions{
		PBFPath: pbf, StorePath: storePath, WorkDir: filepath.Join(root, "work"),
		RunMemoryBytes: 24, PackSegmentBytes: 1_024, MaxOpenShards: 2, Resume: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Layout != worldgraph.PackedLayout || len(manifest.Shards) != 3 || len(manifest.Tiles) != 0 || len(manifest.Regions[0].Tiles) != 0 {
		t.Fatalf("packed manifest = %+v", manifest)
	}
	router, err := worldgraph.OpenRouter(storePath, worldgraph.RouterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	server := httptest.NewServer(pathhttp.NewServerWithHost(router).Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/config")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		CenterLat float64 `json:"center_lat"`
		CenterLon float64 `json:"center_lon"`
		Zoom      int     `json:"zoom"`
		Chunks    struct {
			Generation string `json:"generation"`
			Zoom       int    `json:"zoom"`
			MinZoom    int    `json:"min_render_zoom"`
		} `json:"graph_chunks"`
	}
	if err := json.NewDecoder(response.Body).Decode(&config); err != nil {
		_ = response.Body.Close()
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || config.Chunks.Generation != manifest.Generation || config.Zoom < config.Chunks.MinZoom ||
		config.CenterLat == -8.0540 && config.CenterLon == -34.8800 {
		t.Fatalf("config = %+v, status %d", config, response.StatusCode)
	}

	regions := []struct {
		fromLon, fromLat float64
		toLon, toLat     float64
	}{
		{12.56, 55.67, 12.57, 55.67},
		{-34.90, -8.05, -34.89, -8.04},
		{139.69, 35.68, 139.70, 35.69},
	}
	var tiles []worldgraph.TileID
	for _, region := range regions {
		tile, err := worldgraph.TileForPosition(region.fromLon, region.fromLat, worldgraph.GlobalRoutingZoom)
		if err != nil {
			t.Fatal(err)
		}
		tiles = append(tiles, tile)
		chunkURL := fmt.Sprintf("%s/graph/chunks/%s/%d/%d/%d", server.URL, manifest.Generation, tile.Z, tile.X, tile.Y)
		type result struct {
			status int
			cache  string
			etag   string
			body   string
			err    error
		}
		results := make(chan result, 6)
		var wait sync.WaitGroup
		for range 6 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				response, err := http.Get(chunkURL)
				if err != nil {
					results <- result{err: err}
					return
				}
				body, readErr := io.ReadAll(response.Body)
				_ = response.Body.Close()
				results <- result{status: response.StatusCode, cache: response.Header.Get("Cache-Control"), etag: response.Header.Get("ETag"), body: string(body), err: readErr}
			}()
		}
		wait.Wait()
		close(results)
		for got := range results {
			if got.err != nil || got.status != http.StatusOK || !strings.Contains(got.cache, "immutable") || got.etag != fmt.Sprintf("%q", manifest.Generation) || !strings.Contains(got.body, "FeatureCollection") {
				t.Fatalf("chunk response = %+v", got)
			}
		}
		for _, mode := range []string{"walk", "car"} {
			registered, ok := plugins.Default.Mode(mode)
			if !ok {
				t.Fatalf("mode %q is not registered", mode)
			}
			if _, err := registered.Route(ctx, router, core.ModeRequest{
				From: core.Position{region.fromLon, region.fromLat},
				To:   core.Position{region.toLon, region.toLat},
			}); err != nil {
				t.Fatalf("%s direct route for %+v: %v", mode, region, err)
			}
			endpoint := fmt.Sprintf("%s/mode-route?mode=%s&from=%s&to=%s", server.URL, mode,
				url.QueryEscape(fmt.Sprintf("%.6f,%.6f", region.fromLon, region.fromLat)),
				url.QueryEscape(fmt.Sprintf("%.6f,%.6f", region.toLon, region.toLat)))
			response, err := http.Get(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("%s route status = %d: %s", mode, response.StatusCode, body)
			}
		}
	}

	uncovered, err := worldgraph.TileForPosition(0, 0, worldgraph.GlobalRoutingZoom)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{
		fmt.Sprintf("/graph/chunks/%s/%d/%d/%d", manifest.Generation, uncovered.Z, uncovered.X, uncovered.Y): http.StatusNotFound,
		fmt.Sprintf("/graph/chunks/stale/%d/%d/%d", tiles[0].Z, tiles[0].X, tiles[0].Y):                      http.StatusConflict,
	} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("GET %s status = %d, want %d", path, response.StatusCode, want)
		}
	}

	corruptTile := tiles[2]
	shardX := corruptTile.X >> (worldgraph.GlobalRoutingZoom - worldgraph.PackedShardZoom)
	shardY := corruptTile.Y >> (worldgraph.GlobalRoutingZoom - worldgraph.PackedShardZoom)
	packPath := filepath.Join(storePath, "generations", manifest.Generation, "shards", fmt.Sprint(shardX>>4), fmt.Sprint(shardX), fmt.Sprintf("%d-000.pack", shardY))
	data, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(packPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	corruptRouter, err := worldgraph.OpenRouter(storePath, worldgraph.RouterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer corruptRouter.Close()
	corruptServer := httptest.NewServer(pathhttp.NewServerWithHost(corruptRouter).Handler())
	defer corruptServer.Close()
	response, err = http.Get(fmt.Sprintf("%s/graph/chunks/%s/%d/%d/%d", corruptServer.URL, manifest.Generation, corruptTile.Z, corruptTile.X, corruptTile.Y))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("corrupt chunk status = %d, want 503", response.StatusCode)
	}
}

func writeGlobalE2EPBF(t *testing.T, path string) {
	t.Helper()
	type node struct {
		id       int64
		lon, lat float64
	}
	type way struct {
		id       int64
		from, to int64
		name     string
	}
	nodes := []node{
		{1, 12.56, 55.67}, {2, 12.57, 55.67},
		{3, -34.90, -8.05}, {4, -34.89, -8.04},
		{5, 139.69, 35.68}, {6, 139.70, 35.69},
	}
	ways := []way{{100, 1, 2, "Copenhagen"}, {101, 3, 4, "Recife"}, {102, 5, 6, "Tokyo"}}
	stringsTable := []string{"", "highway", "residential", "name", "Copenhagen", "Recife", "Tokyo"}
	var table []byte
	for _, value := range stringsTable {
		table = appendPBFBytes(table, 1, []byte(value))
	}
	stringIDs := map[string]uint64{"highway": 1, "residential": 2, "name": 3, "Copenhagen": 4, "Recife": 5, "Tokyo": 6}
	var ids, lats, lons []int64
	var previousID, previousLat, previousLon int64
	for _, current := range nodes {
		lat, lon := pbfCoordinate(current.lat), pbfCoordinate(current.lon)
		ids = append(ids, current.id-previousID)
		lats = append(lats, lat-previousLat)
		lons = append(lons, lon-previousLon)
		previousID, previousLat, previousLon = current.id, lat, lon
	}
	var dense []byte
	dense = appendPBFBytes(dense, 1, packedPBFSInt64(ids))
	dense = appendPBFBytes(dense, 8, packedPBFSInt64(lats))
	dense = appendPBFBytes(dense, 9, packedPBFSInt64(lons))
	nodeGroup := appendPBFBytes(nil, 2, dense)
	var wayGroup []byte
	for _, current := range ways {
		encoded := protowire.AppendTag(nil, 1, protowire.VarintType)
		encoded = protowire.AppendVarint(encoded, uint64(current.id))
		encoded = appendPBFBytes(encoded, 2, packedPBFVarints(stringIDs["highway"], stringIDs["name"]))
		encoded = appendPBFBytes(encoded, 3, packedPBFVarints(stringIDs["residential"], stringIDs[current.name]))
		encoded = appendPBFBytes(encoded, 8, packedPBFSInt64([]int64{current.from, current.to - current.from}))
		wayGroup = appendPBFBytes(wayGroup, 3, encoded)
	}
	block := appendPBFBytes(nil, 1, table)
	block = appendPBFBytes(block, 2, nodeGroup)
	block = appendPBFBytes(block, 2, wayGroup)
	var header []byte
	header = appendPBFString(header, 4, "OsmSchema-V0.6")
	header = appendPBFString(header, 4, "DenseNodes")
	data := appendPBFFileBlock(nil, "OSMHeader", header)
	data = appendPBFFileBlock(data, "OSMData", block)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func packedPBFVarints(values ...uint64) []byte {
	var packed []byte
	for _, value := range values {
		packed = protowire.AppendVarint(packed, value)
	}
	return packed
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
