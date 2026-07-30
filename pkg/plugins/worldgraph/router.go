package worldgraph

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
	"github.com/danielscoffee/pathcraft/internal/routing/street"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

const (
	defaultChunkCacheBytes    = int64(512 << 20)
	defaultRouteMaxExpansions = 3
)

type CacheOptions struct {
	MaxBytes int64
}

type RouterOptions struct {
	CacheBytes    int64
	MaxTiles      int
	MaxExpansions int
}

type Router struct {
	store      *Store
	manifest   Manifest
	covered    map[TileID]struct{}
	cache      *chunkCache
	options    RouterOptions
	graphMu    sync.Mutex
	nodeOwners map[int64]TileID
	scanIndex  int
	closed     atomic.Bool
}

func OpenRouter(storePath string, options RouterOptions) (*Router, error) {
	if options.CacheBytes < 0 || options.MaxTiles < 0 || options.MaxExpansions < 0 {
		return nil, fmt.Errorf("worldgraph router options must not be negative")
	}
	if options.CacheBytes == 0 {
		options.CacheBytes = defaultChunkCacheBytes
	}
	if options.MaxTiles == 0 {
		options.MaxTiles = defaultRouteMaxTiles
	}
	if options.MaxExpansions == 0 {
		options.MaxExpansions = defaultRouteMaxExpansions
	}
	store, err := OpenStore(storePath)
	if err != nil {
		return nil, err
	}
	manifest, err := store.Manifest()
	if err != nil {
		return nil, err
	}
	covered := make(map[TileID]struct{}, len(manifest.Tiles))
	for _, tile := range manifest.Tiles {
		covered[tile] = struct{}{}
	}
	return &Router{
		store: store, manifest: manifest, covered: covered,
		cache: newChunkCache(options.CacheBytes), options: options,
		nodeOwners: make(map[int64]TileID),
	}, nil
}

func (r *Router) Close() error {
	if r == nil || r.closed.Swap(true) {
		return nil
	}
	r.cache.Close()
	return nil
}

func (r *Router) RouteByCoordinatesContext(ctx context.Context, request engine.CoordinateRouteRequest) (*engine.CoordinateRouteResult, error) {
	if err := r.checkContext(ctx); err != nil {
		return nil, err
	}
	fromTile, err := r.tileForPosition(request.FromLat, request.FromLon)
	if err != nil {
		return nil, fmt.Errorf("from coordinates: %w", err)
	}
	toTile, err := r.tileForPosition(request.ToLat, request.ToLon)
	if err != nil {
		return nil, fmt.Errorf("to coordinates: %w", err)
	}
	core, err := corridor(fromTile, toTile, 0, r.options.MaxTiles)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRouteAreaLimit, err)
	}
	for _, tile := range core {
		if !r.isCovered(tile) {
			return nil, fmt.Errorf("%w: %+v", ErrUncoveredTile, tile)
		}
	}

	var routeErr error
	for ring := 1; ring <= r.options.MaxExpansions+1; ring++ {
		if err := r.checkContext(ctx); err != nil {
			return nil, err
		}
		tiles, err := Expand(core, ring, r.options.MaxTiles)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrRouteAreaLimit, err)
		}
		g, err := r.graphForTiles(ctx, r.coveredTiles(tiles))
		if err != nil {
			return nil, err
		}
		if len(g.Nodes) == 0 {
			routeErr = astar.ErrNoPath
			continue
		}
		result, err := street.Route(ctx, g, street.Request{
			FromLat: request.FromLat, FromLon: request.FromLon,
			ToLat: request.ToLat, ToLon: request.ToLon,
			Profile: request.Profile, IncludeCoordinates: request.IncludeCoordinates,
		})
		if err == nil {
			return coordinateRouteResult(request, result), nil
		}
		if !errors.Is(err, astar.ErrNoPath) {
			return nil, err
		}
		routeErr = err
	}
	return nil, fmt.Errorf("%w: %w", ErrNoPath, routeErr)
}

func (r *Router) NearestPosition(ctx context.Context, lat, lon float64) (id int64, snapLat, snapLon, distance float64, err error) {
	if err := r.checkContext(ctx); err != nil {
		return 0, 0, 0, 0, err
	}
	tile, err := r.tileForPosition(lat, lon)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if !r.isCovered(tile) {
		return 0, 0, 0, 0, fmt.Errorf("%w: %+v", ErrUncoveredTile, tile)
	}
	tiles, err := Expand([]TileID{tile}, 1, r.options.MaxTiles)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("%w: %v", ErrRouteAreaLimit, err)
	}
	g, err := r.graphForTiles(ctx, r.coveredTiles(tiles))
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if len(g.Nodes) == 0 {
		return 0, 0, 0, 0, ErrNoPath
	}
	nearest, distance := g.NearestNode(lat, lon, geo.HaversineDistance)
	if err := r.checkContext(ctx); err != nil {
		return 0, 0, 0, 0, err
	}
	node := g.Nodes[nearest]
	return int64(nearest), node.Lat, node.Lon, distance, nil
}

func (r *Router) ChunkConfig() (generation string, zoom, minRenderZoom int) {
	if r == nil {
		return "", 0, 0
	}
	minRenderZoom = r.manifest.Zoom - 1
	if minRenderZoom < 0 {
		minRenderZoom = 0
	}
	return r.manifest.Generation, r.manifest.Zoom, minRenderZoom
}

func (r *Router) graphForTiles(ctx context.Context, tiles []TileID) (*graph.Graph, error) {
	nodes := make(map[int64]Node)
	edges := make(map[EdgeID]Edge)
	for _, tile := range tiles {
		if err := r.checkContext(ctx); err != nil {
			return nil, err
		}
		chunk, err := r.loadChunk(ctx, tile)
		if err != nil {
			return nil, err
		}
		for _, node := range chunk.Nodes {
			if previous, exists := nodes[node.ID]; exists && previous != node {
				return nil, fmt.Errorf("%w: conflicting node %d", ErrCorruptChunk, node.ID)
			}
			nodes[node.ID] = node
		}
		for _, edge := range chunk.Edges {
			if previous, exists := edges[edge.ID]; exists && !reflect.DeepEqual(previous, edge) {
				return nil, fmt.Errorf("%w: conflicting edge %+v", ErrCorruptChunk, edge.ID)
			}
			edges[edge.ID] = edge
		}
	}

	g := graph.NewGraph()
	// ponytail: bounded request-local graph uses linear candidates; add wrapped spatial index if snapping profiles show cost.
	g.SetNearestNodeIndex(&linearNearestNodeIndex{})
	nodeIDs := make([]int64, 0, len(nodes))
	for id := range nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })
	for _, id := range nodeIDs {
		node := nodes[id]
		g.AddNode(graph.NodeID(id), node.Lat, node.Lon)
	}
	edgeIDs := make([]EdgeID, 0, len(edges))
	for id := range edges {
		edgeIDs = append(edgeIDs, id)
	}
	sort.Slice(edgeIDs, func(i, j int) bool {
		if edgeIDs[i].WayID != edgeIDs[j].WayID {
			return edgeIDs[i].WayID < edgeIDs[j].WayID
		}
		if edgeIDs[i].From != edgeIDs[j].From {
			return edgeIDs[i].From < edgeIDs[j].From
		}
		return edgeIDs[i].To < edgeIDs[j].To
	})
	for _, id := range edgeIDs {
		edge := edges[id]
		restricted := make([]graph.RestrictedMode, 0, 2)
		if edge.RestrictWalking {
			restricted = append(restricted, graph.RestrictedWalking)
		}
		if edge.RestrictDriving {
			restricted = append(restricted, graph.RestrictedDriving)
		}
		g.AddRestrictedEdgeWithMeta(graph.NodeID(id.From), graph.NodeID(id.To), edge.DistanceMeters, edge.Highway, edge.Name, restricted...)
	}
	return g, nil
}

func (r *Router) loadChunk(ctx context.Context, tile TileID) (*Chunk, error) {
	if err := r.checkContext(ctx); err != nil {
		return nil, err
	}
	return r.cache.GetContext(ctx, tile, func(loadCtx context.Context) (*Chunk, error) {
		chunk, err := r.store.LoadChunkContext(loadCtx, tile)
		if err != nil {
			return nil, err
		}
		return chunk, nil
	})
}

func (r *Router) coveredTiles(tiles []TileID) []TileID {
	covered := make([]TileID, 0, len(tiles))
	for _, tile := range tiles {
		if r.isCovered(tile) {
			covered = append(covered, tile)
		}
	}
	return covered
}

func (r *Router) isCovered(tile TileID) bool {
	_, ok := r.covered[tile]
	return ok
}

func (r *Router) tileForPosition(lat, lon float64) (TileID, error) {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lon) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return TileID{}, fmt.Errorf("%w: coordinates must be finite latitude [-90,90] and longitude [-180,180]", ErrInvalidPosition)
	}
	tile, err := TileForPosition(lon, lat, r.manifest.Zoom)
	if err != nil {
		return TileID{}, fmt.Errorf("%w: %v", ErrInvalidPosition, err)
	}
	return tile, nil
}

func (r *Router) checkContext(ctx context.Context) error {
	if r == nil || r.closed.Load() {
		return ErrRouterClosed
	}
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

type linearNearestNodeIndex struct {
	ids []int64
}

func (index *linearNearestNodeIndex) Insert(node plugins.IndexedNode) {
	index.ids = append(index.ids, node.ID)
}

func (index *linearNearestNodeIndex) Rebuild(nodes []plugins.IndexedNode) {
	index.ids = index.ids[:0]
	for _, node := range nodes {
		index.Insert(node)
	}
}

func (index *linearNearestNodeIndex) NearestCandidates(_, _ float64) []int64 {
	return index.ids
}

func coordinateRouteResult(request engine.CoordinateRouteRequest, result *street.Result) *engine.CoordinateRouteResult {
	var coordinates []engine.Coordinate
	if len(result.Coordinates) > 0 {
		coordinates = make([]engine.Coordinate, len(result.Coordinates))
		for index, coordinate := range result.Coordinates {
			coordinates[index] = engine.Coordinate{Lat: coordinate.Lat, Lon: coordinate.Lon}
		}
	}
	if request.IncludeCoordinates && request.IncludeInputInShape {
		coordinates = append([]engine.Coordinate{{Lat: request.FromLat, Lon: request.FromLon}}, coordinates...)
		coordinates = append(coordinates, engine.Coordinate{Lat: request.ToLat, Lon: request.ToLon})
	}
	return &engine.CoordinateRouteResult{
		RouteResult: engine.RouteResult{
			Nodes: result.Nodes, Coordinates: coordinates,
			Distance: result.Distance, Duration: result.Duration,
		},
		FromNodeID: result.FromNodeID, ToNodeID: result.ToNodeID,
		FromSnapDistanceM: result.FromSnapDistanceM, ToSnapDistanceM: result.ToSnapDistanceM,
	}
}
