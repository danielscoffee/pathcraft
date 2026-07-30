package worldgraph

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func TestRouterRoutesWithinOneTile(t *testing.T) {
	tile := TileID{Z: 4, X: 8, Y: 8}
	from := routerNode(tile, 1, 0.25, 0.5)
	to := routerNode(tile, 2, 0.75, 0.5)
	edges := routerBidirectionalEdges(t, from, to, 10, false, false)
	dir, manifest := publishRouterFixture(t, map[TileID]Chunk{tile: {Tile: tile, Nodes: []Node{from, to}, Edges: edges}})
	router := openTestRouter(t, dir, RouterOptions{})

	result, err := router.RouteByCoordinatesContext(context.Background(), routerRequest(from, to, mobility.NewWalking(1.4)))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || result.Nodes[0] != from.ID || result.Nodes[1] != to.ID || len(result.Coordinates) != 2 {
		t.Fatalf("route = %+v", result)
	}
	id, _, _, distance, err := router.NearestPosition(context.Background(), from.Lat, from.Lon)
	if err != nil || id != from.ID || distance != 0 {
		t.Fatalf("NearestPosition() = %d, %v, %v", id, distance, err)
	}
	generation, zoom, minRenderZoom := router.ChunkConfig()
	if generation != manifest.Generation || zoom != tile.Z || minRenderZoom != tile.Z-1 {
		t.Fatalf("ChunkConfig() = %q, %d, %d", generation, zoom, minRenderZoom)
	}
}

func TestRouterRejectsUnsupportedMercatorLatitude(t *testing.T) {
	router := &Router{manifest: Manifest{Zoom: DefaultZoom}}
	for _, latitude := range []float64{-90, 90} {
		if _, err := router.tileForPosition(latitude, 0); !errors.Is(err, ErrInvalidPosition) {
			t.Fatalf("tileForPosition(%v, 0) error = %v, want ErrInvalidPosition", latitude, err)
		}
	}
}

func TestRouterRoutesAcrossChunkSeam(t *testing.T) {
	left := TileID{Z: 4, X: 7, Y: 8}
	right := TileID{Z: 4, X: 8, Y: 8}
	from := routerNode(left, 1, 0.9, 0.5)
	to := routerNode(right, 2, 0.1, 0.5)
	edges := routerBidirectionalEdges(t, from, to, 20, false, false)
	chunks := map[TileID]Chunk{
		left:  {Tile: left, Nodes: []Node{from, to}, Edges: edges},
		right: {Tile: right, Nodes: []Node{from, to}, Edges: edges},
	}
	dir, _ := publishRouterFixture(t, chunks)
	router := openTestRouter(t, dir, RouterOptions{MaxTiles: 32})

	result, err := router.RouteByCoordinatesContext(context.Background(), routerRequest(from, to, mobility.NewDriving(8.3)))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || result.FromNodeID != from.ID || result.ToNodeID != to.ID {
		t.Fatalf("cross-seam route = %+v", result)
	}
}

func TestRouterRoutesPackedAcrossShardBoundary(t *testing.T) {
	left := TileID{Z: GlobalRoutingZoom, X: 15, Y: 2048}
	right := TileID{Z: GlobalRoutingZoom, X: 16, Y: 2048}
	from := routerNode(left, 1, 0.9, 0.5)
	to := routerNode(right, 2, 0.1, 0.5)
	edges := routerBidirectionalEdges(t, from, to, 20, false, false)
	for index := range edges {
		edges[index].Sources = []string{"planet"}
	}
	root, _ := writePackedStoreFixture(t, map[TileID]Chunk{
		left:  {Tile: left, Nodes: []Node{from, to}, Edges: edges},
		right: {Tile: right, Nodes: []Node{from, to}, Edges: edges},
	})
	router := openTestRouter(t, root, RouterOptions{MaxTiles: 32})
	if len(router.covered) != 0 {
		t.Fatalf("packed router materialized %d covered tiles", len(router.covered))
	}

	result, err := router.RouteByCoordinatesContext(context.Background(), routerRequest(from, to, mobility.NewDriving(8.3)))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 2 || result.FromNodeID != from.ID || result.ToNodeID != to.ID {
		t.Fatalf("cross-shard route = %+v", result)
	}
	if _, err := router.ChunkGeoJSON(context.Background(), left.Z, left.X, left.Y); err != nil {
		t.Fatalf("ChunkGeoJSON() error = %v", err)
	}

	outsideTile := right
	outsideTile.X++
	outside := routerNode(outsideTile, 3, 0.5, 0.5)
	if _, err := router.RouteByCoordinatesContext(context.Background(), routerRequest(from, outside, mobility.NewDriving(8.3))); !errors.Is(err, ErrUncoveredTile) {
		t.Fatalf("uncovered route error = %v, want ErrUncoveredTile", err)
	}

	if err := router.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := router.store.Covers(context.Background(), left); !errors.Is(err, ErrRouterClosed) {
		t.Fatalf("closed store Covers() error = %v, want ErrRouterClosed", err)
	}
}

func TestRouterPackedCoveragePropagatesCorruptIndex(t *testing.T) {
	shard := TileID{Z: PackedShardZoom, X: 1, Y: 128}
	tile := mustPackedTile(t, shard, 0)
	root, manifest := writePackedStoreFixture(t, map[TileID]Chunk{tile: {Tile: tile}})
	prefix := packedFixturePrefix(t, root, manifest, shard)
	if err := os.WriteFile(shardIndexPath(prefix), []byte("bad index"), 0o600); err != nil {
		t.Fatal(err)
	}
	router := openTestRouter(t, root, RouterOptions{})
	if _, err := router.ChunkGeoJSON(context.Background(), tile.Z, tile.X, tile.Y); !errors.Is(err, ErrCorruptChunk) {
		t.Fatalf("ChunkGeoJSON() error = %v, want ErrCorruptChunk", err)
	}
}

func TestRouterChunkViewportHandlesPackedAntimeridianCoverage(t *testing.T) {
	westShard := TileID{Z: PackedShardZoom, X: 0, Y: 128}
	eastShard := TileID{Z: PackedShardZoom, X: 255, Y: 128}
	westTile := mustPackedTile(t, westShard, 0)
	eastTile := mustPackedTile(t, eastShard, 255)
	root, _ := writePackedStoreFixture(t, map[TileID]Chunk{
		westTile: {Tile: westTile},
		eastTile: {Tile: eastTile},
	})
	router := openTestRouter(t, root, RouterOptions{})
	lat, lon, zoom := router.ChunkViewport()
	_, _, minRenderZoom := router.ChunkConfig()
	if lat < -2 || lat > 2 || math.Abs(lon) < 170 || zoom < minRenderZoom {
		t.Fatalf("ChunkViewport() = %v, %v, %d; min render %d", lat, lon, zoom, minRenderZoom)
	}
}

func TestRouterChunkViewportSupportsLegacyTiles(t *testing.T) {
	west := TileID{Z: GlobalRoutingZoom, X: 0, Y: 2048}
	east := TileID{Z: GlobalRoutingZoom, X: (1 << GlobalRoutingZoom) - 1, Y: 2048}
	root, _ := publishRouterFixture(t, map[TileID]Chunk{
		west: {Tile: west},
		east: {Tile: east},
	})
	router := openTestRouter(t, root, RouterOptions{})
	_, lon, zoom := router.ChunkViewport()
	_, _, minRenderZoom := router.ChunkConfig()
	if math.Abs(lon) < 170 || zoom < minRenderZoom {
		t.Fatalf("ChunkViewport() longitude = %v, zoom = %d", lon, zoom)
	}
}

func TestRouterHonorsRestrictionsAndOnewayEdges(t *testing.T) {
	tile := TileID{Z: 4, X: 8, Y: 8}
	nodes := []Node{
		routerNode(tile, 1, 0.1, 0.2),
		routerNode(tile, 2, 0.4, 0.2),
		routerNode(tile, 3, 0.6, 0.8),
		routerNode(tile, 4, 0.9, 0.8),
	}
	oneway := routerBidirectionalEdges(t, nodes[0], nodes[1], 30, false, false)
	oneway[1].RestrictDriving = true
	walkingRestricted := routerDirectedEdge(t, nodes[2], nodes[3], 31, true, false)
	dir, _ := publishRouterFixture(t, map[TileID]Chunk{tile: {
		Tile: tile, Nodes: nodes, Edges: append(oneway, walkingRestricted),
	}})
	router := openTestRouter(t, dir, RouterOptions{})

	if _, err := router.RouteByCoordinatesContext(context.Background(), routerRequest(nodes[1], nodes[0], mobility.NewWalking(1.4))); err != nil {
		t.Fatalf("walking against oneway: %v", err)
	}
	if _, err := router.RouteByCoordinatesContext(context.Background(), routerRequest(nodes[1], nodes[0], mobility.NewDriving(8.3))); !errors.Is(err, ErrNoPath) {
		t.Fatalf("driving against oneway error = %v, want ErrNoPath", err)
	}
	if _, err := router.RouteByCoordinatesContext(context.Background(), routerRequest(nodes[2], nodes[3], mobility.NewWalking(1.4))); !errors.Is(err, ErrNoPath) {
		t.Fatalf("walking restricted edge error = %v, want ErrNoPath", err)
	}
	if _, err := router.RouteByCoordinatesContext(context.Background(), routerRequest(nodes[2], nodes[3], mobility.NewDriving(8.3))); err != nil {
		t.Fatalf("driving allowed edge: %v", err)
	}
}

func TestRouterExpandsCorridorOnlyAfterNoPath(t *testing.T) {
	core := TileID{Z: 4, X: 5, Y: 5}
	detour := TileID{Z: 4, X: 7, Y: 5}
	from := routerNode(core, 1, 0.2, 0.4)
	to := routerNode(core, 2, 0.8, 0.6)
	middle := routerNode(detour, 3, 0.5, 0.5)
	edges := []Edge{
		routerDirectedEdge(t, from, middle, 40, false, false),
		routerDirectedEdge(t, middle, to, 41, false, false),
	}
	chunks := map[TileID]Chunk{
		core:   {Tile: core},
		detour: {Tile: detour, Nodes: []Node{from, to, middle}, Edges: edges},
	}
	dir, _ := publishRouterFixture(t, chunks)
	router := openTestRouter(t, dir, RouterOptions{MaxTiles: 64, MaxExpansions: 1})

	result, err := router.RouteByCoordinatesContext(context.Background(), routerRequest(from, to, mobility.NewWalking(1.4)))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) != 3 || result.Nodes[1] != middle.ID {
		t.Fatalf("expanded route = %+v", result)
	}
}

func TestRouterUsesWrappedAntimeridianCorridor(t *testing.T) {
	west := TileID{Z: 3, X: 7, Y: 4}
	east := TileID{Z: 3, X: 0, Y: 4}
	from := routerNode(west, 1, 0.9, 0.5)
	to := routerNode(east, 2, 0.1, 0.5)
	edges := routerBidirectionalEdges(t, from, to, 50, false, false)
	dir, _ := publishRouterFixture(t, map[TileID]Chunk{
		west: {Tile: west, Nodes: []Node{from, to}, Edges: edges},
		east: {Tile: east, Nodes: []Node{from, to}, Edges: edges},
	})
	router := openTestRouter(t, dir, RouterOptions{MaxTiles: 32})
	if _, err := router.RouteByCoordinatesContext(context.Background(), routerRequest(from, to, mobility.NewWalking(1.4))); err != nil {
		t.Fatal(err)
	}
}

func TestRouterSnapsAcrossAntimeridian(t *testing.T) {
	west := TileID{Z: 3, X: 7, Y: 4}
	east := TileID{Z: 3, X: 0, Y: 4}
	acrossSeam := Node{ID: 1, Lat: -10, Lon: 179.9, Owner: west}
	sameSideDecoy := Node{ID: 2, Lat: -10, Lon: -170, Owner: east}
	target := Node{ID: 3, Lat: -10, Lon: 170, Owner: west}
	edge := routerDirectedEdge(t, acrossSeam, target, 51, false, false)
	nodes := []Node{acrossSeam, sameSideDecoy, target}
	dir, _ := publishRouterFixture(t, map[TileID]Chunk{
		west: {Tile: west, Nodes: nodes, Edges: []Edge{edge}},
		east: {Tile: east, Nodes: nodes, Edges: []Edge{edge}},
	})
	router := openTestRouter(t, dir, RouterOptions{MaxTiles: 32})
	request := engine.CoordinateRouteRequest{
		FromLat: -10, FromLon: -179.8, ToLat: target.Lat, ToLon: target.Lon,
		Profile: mobility.NewWalking(1.4), IncludeCoordinates: true,
	}
	result, err := router.RouteByCoordinatesContext(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.FromNodeID != acrossSeam.ID {
		t.Fatalf("FromNodeID = %d, want wrapped nearest %d", result.FromNodeID, acrossSeam.ID)
	}
}

func TestRouterDistinguishesCoverageLimitsNoPathCorruptionAndCancellation(t *testing.T) {
	tile := TileID{Z: 4, X: 8, Y: 8}
	other := TileID{Z: 4, X: 9, Y: 8}
	from := routerNode(tile, 1, 0.2, 0.5)
	to := routerNode(tile, 2, 0.8, 0.5)
	dir, manifest := publishRouterFixture(t, map[TileID]Chunk{tile: {Tile: tile, Nodes: []Node{from, to}}})
	router := openTestRouter(t, dir, RouterOptions{})

	outside := routerNode(other, 3, 0.5, 0.5)
	if _, err := router.RouteByCoordinatesContext(context.Background(), routerRequest(from, outside, mobility.NewWalking(1.4))); !errors.Is(err, ErrUncoveredTile) {
		t.Fatalf("missing coverage error = %v", err)
	}
	if _, err := router.RouteByCoordinatesContext(context.Background(), routerRequest(from, to, mobility.NewWalking(1.4))); !errors.Is(err, ErrNoPath) {
		t.Fatalf("no path error = %v", err)
	}

	limited := openTestRouter(t, dir, RouterOptions{MaxTiles: 1})
	if _, err := limited.RouteByCoordinatesContext(context.Background(), routerRequest(from, to, mobility.NewWalking(1.4))); !errors.Is(err, ErrRouteAreaLimit) {
		t.Fatalf("area limit error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := router.RouteByCoordinatesContext(ctx, routerRequest(from, to, mobility.NewWalking(1.4))); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}

	corruptPath := filepath.Join(dir, "generations", manifest.Generation, tilePath(tile))
	data, err := os.ReadFile(corruptPath)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(corruptPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt := openTestRouter(t, dir, RouterOptions{})
	if _, err := corrupt.RouteByCoordinatesContext(context.Background(), routerRequest(from, to, mobility.NewWalking(1.4))); !errors.Is(err, ErrCorruptChunk) {
		t.Fatalf("corrupt chunk error = %v", err)
	}
}

func TestRouterChunkGeoJSONRendersOwnedEdges(t *testing.T) {
	tile := TileID{Z: 4, X: 8, Y: 8}
	from := routerNode(tile, 1, 0.2, 0.5)
	to := routerNode(tile, 2, 0.8, 0.5)
	edges := routerBidirectionalEdges(t, from, to, 60, false, false)
	dir, _ := publishRouterFixture(t, map[TileID]Chunk{tile: {Tile: tile, Nodes: []Node{from, to}, Edges: edges}})
	router := openTestRouter(t, dir, RouterOptions{})
	data, err := router.ChunkGeoJSON(context.Background(), tile.Z, tile.X, tile.Y)
	if err != nil {
		t.Fatal(err)
	}
	var collection struct {
		Type     string            `json:"type"`
		Features []json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(data, &collection); err != nil {
		t.Fatal(err)
	}
	if collection.Type != "FeatureCollection" || len(collection.Features) != 2 {
		t.Fatalf("GeoJSON = %s", data)
	}
}

func publishRouterFixture(t *testing.T, chunks map[TileID]Chunk) (string, Manifest) {
	t.Helper()
	tiles := make([]TileID, 0, len(chunks))
	for tile := range chunks {
		tiles = append(tiles, tile)
	}
	sortTiles(tiles)
	manifest := Manifest{
		Generation: "router-test",
		Zoom:       tiles[0].Z,
		BuiltAt:    time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC),
		Regions: []RegionManifest{{
			Name: "test", SourceSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Tiles: tiles,
		}},
		Tiles: tiles,
	}
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishGeneration(manifest, chunks); err != nil {
		t.Fatal(err)
	}
	published, err := store.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	return dir, published
}

func openTestRouter(t *testing.T, path string, options RouterOptions) *Router {
	t.Helper()
	router, err := OpenRouter(path, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

func routerNode(tile TileID, id int64, xFraction, yFraction float64) Node {
	bounds := tile.Bounds()
	return Node{
		ID:    id,
		Lon:   bounds.West + (bounds.East-bounds.West)*xFraction,
		Lat:   bounds.South + (bounds.North-bounds.South)*yFraction,
		Owner: tile,
	}
}

func routerDirectedEdge(t *testing.T, from, to Node, wayID int64, restrictWalking, restrictDriving bool) Edge {
	t.Helper()
	owner, err := TileForEdge(from, to, from.Owner.Z)
	if err != nil {
		t.Fatal(err)
	}
	return Edge{
		ID: EdgeID{WayID: wayID, From: from.ID, To: to.ID}, DistanceMeters: 100,
		Highway: "residential", RestrictWalking: restrictWalking, RestrictDriving: restrictDriving,
		Owner: owner, Sources: []string{"test"},
	}
}

func routerBidirectionalEdges(t *testing.T, from, to Node, wayID int64, restrictWalking, restrictDriving bool) []Edge {
	return []Edge{
		routerDirectedEdge(t, from, to, wayID, restrictWalking, restrictDriving),
		routerDirectedEdge(t, to, from, wayID, restrictWalking, restrictDriving),
	}
}

func routerRequest(from, to Node, profile mobility.Profile) engine.CoordinateRouteRequest {
	return engine.CoordinateRouteRequest{
		FromLat: from.Lat, FromLon: from.Lon, ToLat: to.Lat, ToLon: to.Lon,
		Profile: profile, IncludeCoordinates: true,
	}
}
