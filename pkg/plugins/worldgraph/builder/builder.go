package builder

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danielscoffee/pathcraft/internal/geo"
	internalosm "github.com/danielscoffee/pathcraft/internal/osm"
	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

const (
	contributionBatchSize   = 4_096
	maxWayContributionBytes = 64 << 20
)

type Options struct {
	PBFPath     string
	StorePath   string
	Region      string
	Zoom        int
	TempDir     string
	MaxWayNodes int
}

type scanFunctions struct {
	nodes func(context.Context, string, func([]worldgraph.Node) error) error
	ways  func(context.Context, string, int, func(Way) error) error
}

var defaultScanFunctions = scanFunctions{
	nodes: scanNodesUnchecked,
	ways:  scanWaysUnchecked,
}

func Build(ctx context.Context, options Options) (worldgraph.Manifest, error) {
	return build(ctx, options, defaultScanFunctions)
}

func build(ctx context.Context, options Options, scans scanFunctions) (worldgraph.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return worldgraph.Manifest{}, err
	}
	if err := normalizeBuildOptions(&options); err != nil {
		return worldgraph.Manifest{}, err
	}
	if err := ctx.Err(); err != nil {
		return worldgraph.Manifest{}, err
	}
	if scans.nodes == nil || scans.ways == nil {
		return worldgraph.Manifest{}, fmt.Errorf("build scanner is nil")
	}

	workDir, err := os.MkdirTemp(options.TempDir, "pathcraft-worldgraph-*")
	if err != nil {
		return worldgraph.Manifest{}, err
	}
	defer os.RemoveAll(workDir)

	snapshotPath := filepath.Join(workDir, "input.osm.pbf")
	sourceHash, err := snapshotPBF(ctx, options.PBFPath, snapshotPath)
	if err != nil {
		return worldgraph.Manifest{}, fmt.Errorf("snapshot PBF: %w", err)
	}
	options.PBFPath = snapshotPath
	if err := validatePBF(ctx, options.PBFPath, options.MaxWayNodes); err != nil {
		return worldgraph.Manifest{}, fmt.Errorf("validate PBF: %w", err)
	}

	store, err := worldgraph.OpenStore(options.StorePath)
	if err != nil {
		return worldgraph.Manifest{}, err
	}
	previous, hasPrevious, err := optionalManifest(store)
	if err != nil {
		return worldgraph.Manifest{}, err
	}
	if hasPrevious && previous.Zoom != options.Zoom {
		return worldgraph.Manifest{}, fmt.Errorf("store zoom %d does not match build zoom %d", previous.Zoom, options.Zoom)
	}

	nodes, err := OpenNodeIndex(filepath.Join(workDir, "nodes.db"))
	if err != nil {
		return worldgraph.Manifest{}, err
	}
	defer nodes.Close()
	contributions, err := openContributionStore(filepath.Join(workDir, "contributions.db"))
	if err != nil {
		return worldgraph.Manifest{}, err
	}
	defer contributions.Close()

	if err := scans.nodes(ctx, options.PBFPath, func(batch []worldgraph.Node) error {
		tiles := make([]worldgraph.TileID, len(batch))
		for index := range batch {
			tile, err := worldgraph.TileForPosition(batch[index].Lon, batch[index].Lat, options.Zoom)
			if err != nil {
				return fmt.Errorf("node %d: %w", batch[index].ID, err)
			}
			batch[index].Owner = tile
			tiles[index] = tile
		}
		if err := nodes.PutBatch(ctx, batch); err != nil {
			return err
		}
		return contributions.MarkTiles(ctx, tiles)
	}); err != nil {
		return worldgraph.Manifest{}, fmt.Errorf("scan PBF nodes: %w", err)
	}

	writer := contributionWriter{ctx: ctx, store: contributions}
	if err := scans.ways(ctx, options.PBFPath, options.MaxWayNodes, func(way Way) error {
		return addWayContributions(nodes, &writer, way, options.Region, options.Zoom)
	}); err != nil {
		return worldgraph.Manifest{}, fmt.Errorf("scan PBF ways: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return worldgraph.Manifest{}, err
	}

	newTiles, err := contributions.Tiles(ctx)
	if err != nil {
		return worldgraph.Manifest{}, err
	}
	manifest := replacementManifest(previous, hasPrevious, options, sourceHash, newTiles)
	if err := stageChangedChunks(ctx, store, previous, hasPrevious, manifest, contributions, options.Region, newTiles); err != nil {
		return worldgraph.Manifest{}, err
	}
	if err := ctx.Err(); err != nil {
		return worldgraph.Manifest{}, err
	}
	if err := store.PublishGenerationFromContext(ctx, manifest, func(tile worldgraph.TileID) (worldgraph.Chunk, bool, error) {
		return contributions.StagedChunk(ctx, tile)
	}); err != nil {
		return worldgraph.Manifest{}, err
	}
	return store.Manifest()
}

func normalizeBuildOptions(options *Options) error {
	if options.PBFPath == "" {
		return fmt.Errorf("PBF path is required")
	}
	if options.StorePath == "" {
		return fmt.Errorf("store path is required")
	}
	if strings.TrimSpace(options.Region) == "" {
		return fmt.Errorf("region is required")
	}
	if len(options.Region) > worldgraph.MaxSourceNameBytes {
		return fmt.Errorf("region exceeds %d bytes", worldgraph.MaxSourceNameBytes)
	}
	if options.Zoom == 0 {
		options.Zoom = worldgraph.DefaultZoom
	}
	tile, err := worldgraph.TileForPosition(0, 0, options.Zoom)
	if err != nil {
		return err
	}
	if tile.Z != options.Zoom {
		return fmt.Errorf("invalid zoom %d", options.Zoom)
	}
	if options.MaxWayNodes == 0 {
		options.MaxWayNodes = DefaultMaxWayNodes
	}
	if options.MaxWayNodes < 1 {
		return fmt.Errorf("maximum way node count must be positive")
	}
	return nil
}

func optionalManifest(store *worldgraph.Store) (worldgraph.Manifest, bool, error) {
	manifest, err := store.Manifest()
	if errors.Is(err, os.ErrNotExist) {
		return worldgraph.Manifest{}, false, nil
	}
	return manifest, err == nil, err
}

func snapshotPBF(ctx context.Context, source, destination string) (fingerprint string, err error) {
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	info, err := input.Stat()
	if err != nil {
		return "", errors.Join(err, input.Close())
	}
	if !info.Mode().IsRegular() {
		return "", errors.Join(fmt.Errorf("PBF input is not a regular file"), input.Close())
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", errors.Join(err, input.Close())
	}
	defer func() {
		err = errors.Join(err, output.Close(), input.Close())
		if err != nil {
			_ = os.Remove(destination)
		}
	}()

	hash := sha256.New()
	writer := io.MultiWriter(output, hash)
	buffer := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := input.Read(buffer)
		if count > 0 {
			written, writeErr := writer.Write(buffer[:count])
			if writeErr != nil {
				return "", writeErr
			}
			if written != count {
				return "", io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	if err := output.Sync(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func addWayContributions(index *NodeIndex, writer *contributionWriter, way Way, region string, zoom int) error {
	ctx := context.Background()
	var lookup func(int64) (worldgraph.Node, bool, error)
	var emit func(worldgraph.Node, worldgraph.Node, worldgraph.Edge) error
	if index != nil {
		lookup = index.Get
	}
	if writer != nil {
		ctx = writer.ctx
		emit = writer.Add
	}
	return addWayContributionsWithLookup(ctx, lookup, emit, way, region, zoom)
}

func addWayContributionsWithLookup(
	ctx context.Context,
	lookup func(int64) (worldgraph.Node, bool, error),
	emit func(worldgraph.Node, worldgraph.Node, worldgraph.Edge) error,
	way Way,
	region string,
	zoom int,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	policy := internalosm.PolicyForTags(way.Tags)
	if !policy.Routable || len(way.NodeIDs) < 2 {
		return nil
	}
	if len(way.Tags["highway"]) > worldgraph.MaxEdgeHighwayBytes || len(way.Tags["name"]) > worldgraph.MaxEdgeNameBytes {
		return fmt.Errorf("way %d has routing text above chunk limits", way.ID)
	}
	estimatedBytes := int64(len(way.NodeIDs)-1) * 6 * int64(len(way.Tags["highway"])+len(way.Tags["name"])+len(region)+256)
	if estimatedBytes > maxWayContributionBytes {
		return fmt.Errorf("way %d contributions exceed %d estimated bytes", way.ID, maxWayContributionBytes)
	}
	if lookup == nil || emit == nil {
		return fmt.Errorf("way contribution lookup and sink are required")
	}
	from, found, err := lookup(way.NodeIDs[0])
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("way %d references missing node %d", way.ID, way.NodeIDs[0])
	}
	if !validOSMPosition(from.Lon, from.Lat) {
		return fmt.Errorf("way %d references node %d with invalid coordinates", way.ID, from.ID)
	}
	for _, toID := range way.NodeIDs[1:] {
		if err := ctx.Err(); err != nil {
			return err
		}
		to, found, err := lookup(toID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("way %d references missing node %d", way.ID, toID)
		}
		if !validOSMPosition(to.Lon, to.Lat) {
			return fmt.Errorf("way %d references node %d with invalid coordinates", way.ID, to.ID)
		}
		if from.ID != to.ID && validMercatorPosition(from.Lon, from.Lat) && validMercatorPosition(to.Lon, to.Lat) {
			edges, err := segmentEdges(way, policy, from, to, region, zoom)
			if err != nil {
				return err
			}
			for _, edge := range edges {
				edgeFrom, edgeTo := from, to
				if edge.ID.From == to.ID {
					edgeFrom, edgeTo = to, from
				}
				if err := emit(edgeFrom, edgeTo, edge); err != nil {
					return err
				}
			}
		}
		from = to
	}
	return nil
}

func segmentEdges(way Way, policy internalosm.WayPolicy, from, to worldgraph.Node, region string, zoom int) ([]worldgraph.Edge, error) {
	owner, err := worldgraph.TileForEdge(from, to, zoom)
	if err != nil {
		return nil, err
	}
	distance := geo.HaversineDistance(from.Lat, from.Lon, to.Lat, to.Lon)
	edge := func(source, target int64, restrictDriving bool) worldgraph.Edge {
		return worldgraph.Edge{
			ID:              worldgraph.EdgeID{WayID: way.ID, From: source, To: target},
			DistanceMeters:  distance,
			Highway:         way.Tags["highway"],
			Name:            way.Tags["name"],
			RestrictWalking: policy.RestrictWalking,
			RestrictDriving: policy.RestrictDriving || restrictDriving,
			Owner:           owner,
			Sources:         []string{region},
		}
	}
	forwardRestricted := policy.Direction == internalosm.DirectionReverse
	reverseRestricted := policy.Direction == internalosm.DirectionForward
	return []worldgraph.Edge{
		edge(from.ID, to.ID, forwardRestricted),
		edge(to.ID, from.ID, reverseRestricted),
	}, nil
}

type contributionWriter struct {
	ctx     context.Context
	store   *contributionStore
	pending []edgeContribution
}

func (w *contributionWriter) Add(from, to worldgraph.Node, edge worldgraph.Edge) error {
	w.pending = append(w.pending, edgeContributions(from, to, edge)...)
	if len(w.pending) >= contributionBatchSize {
		return w.Flush()
	}
	return nil
}

func edgeContributions(from, to worldgraph.Node, edge worldgraph.Edge) []edgeContribution {
	tiles := []worldgraph.TileID{from.Owner, to.Owner, edge.Owner}
	seen := make(map[worldgraph.TileID]struct{}, len(tiles))
	contributions := make([]edgeContribution, 0, len(tiles))
	for _, tile := range tiles {
		if _, exists := seen[tile]; exists {
			continue
		}
		seen[tile] = struct{}{}
		contributions = append(contributions, edgeContribution{tile: tile, from: from, to: to, edge: edge})
	}
	return contributions
}

func (w *contributionWriter) Flush() error {
	if len(w.pending) == 0 {
		return w.ctx.Err()
	}
	if err := w.store.PutBatch(w.ctx, w.pending); err != nil {
		return err
	}
	w.pending = w.pending[:0]
	return nil
}

func replacementManifest(previous worldgraph.Manifest, hasPrevious bool, options Options, sourceHash string, newTiles []worldgraph.TileID) worldgraph.Manifest {
	regions := make([]worldgraph.RegionManifest, 0, len(previous.Regions)+1)
	if hasPrevious {
		for _, region := range previous.Regions {
			if region.Name != options.Region {
				regions = append(regions, region)
			}
		}
	}
	regions = append(regions, worldgraph.RegionManifest{
		Name:         options.Region,
		SourceSHA256: sourceHash,
		Tiles:        append([]worldgraph.TileID(nil), newTiles...),
	})
	tileSet := make(map[worldgraph.TileID]struct{})
	for _, region := range regions {
		for _, tile := range region.Tiles {
			tileSet[tile] = struct{}{}
		}
	}
	tiles := tileSetSlice(tileSet)
	previousGeneration := ""
	if hasPrevious {
		previousGeneration = previous.Generation
	}
	return worldgraph.Manifest{
		Generation: generationID(previousGeneration, options.Region, sourceHash, options.Zoom),
		Zoom:       options.Zoom,
		Regions:    regions,
		Tiles:      tiles,
		BuiltAt:    time.Now().UTC(),
	}
}

func generationID(previous, region, sourceHash string, zoom int) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d", previous, region, sourceHash, zoom)))
	return fmt.Sprintf("%x", hash[:16])
}

func stageChangedChunks(
	ctx context.Context,
	store *worldgraph.Store,
	previous worldgraph.Manifest,
	hasPrevious bool,
	next worldgraph.Manifest,
	contributions *contributionStore,
	region string,
	newTiles []worldgraph.TileID,
) error {
	affected := make(map[worldgraph.TileID]struct{}, len(newTiles))
	for _, tile := range newTiles {
		affected[tile] = struct{}{}
	}
	if hasPrevious {
		for _, oldRegion := range previous.Regions {
			if oldRegion.Name == region {
				for _, tile := range oldRegion.Tiles {
					affected[tile] = struct{}{}
				}
				break
			}
		}
	}
	nextTiles := make(map[worldgraph.TileID]struct{}, len(next.Tiles))
	for _, tile := range next.Tiles {
		nextTiles[tile] = struct{}{}
	}
	previousTiles := make(map[worldgraph.TileID]struct{}, len(previous.Tiles))
	for _, tile := range previous.Tiles {
		previousTiles[tile] = struct{}{}
	}

	for _, tile := range tileSetSlice(affected) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, covered := nextTiles[tile]; !covered {
			continue
		}
		chunk := worldgraph.Chunk{Tile: tile}
		if _, existed := previousTiles[tile]; existed {
			loaded, err := store.LoadChunk(tile)
			if err != nil {
				return err
			}
			chunk = removeRegion(*loaded, region)
		}
		incoming, found, err := contributions.Chunk(ctx, tile)
		if err != nil {
			return err
		}
		if found {
			chunk, err = mergeChunks(chunk, incoming)
			if err != nil {
				return err
			}
		}
		if err := contributions.StageChunk(ctx, normalizeChunk(chunk)); err != nil {
			return err
		}
	}
	return nil
}

func removeRegion(chunk worldgraph.Chunk, region string) worldgraph.Chunk {
	kept := chunk.Edges[:0]
	for _, edge := range chunk.Edges {
		sources := edge.Sources[:0]
		for _, source := range edge.Sources {
			if source != region {
				sources = append(sources, source)
			}
		}
		if len(sources) == 0 {
			continue
		}
		edge.Sources = sources
		kept = append(kept, edge)
	}
	chunk.Edges = kept
	return chunk
}

func mergeChunks(base, incoming worldgraph.Chunk) (worldgraph.Chunk, error) {
	if base.Tile != incoming.Tile {
		return worldgraph.Chunk{}, fmt.Errorf("cannot merge chunks %+v and %+v", base.Tile, incoming.Tile)
	}
	nodes := make(map[int64]worldgraph.Node, len(base.Nodes)+len(incoming.Nodes))
	for _, node := range base.Nodes {
		nodes[node.ID] = node
	}
	for _, node := range incoming.Nodes {
		if previous, exists := nodes[node.ID]; exists && previous != node {
			return worldgraph.Chunk{}, fmt.Errorf("conflicting node %d in tile %+v", node.ID, base.Tile)
		}
		nodes[node.ID] = node
	}
	edges := make(map[worldgraph.EdgeID]worldgraph.Edge, len(base.Edges)+len(incoming.Edges))
	for _, edge := range base.Edges {
		edges[edge.ID] = edge
	}
	for _, edge := range incoming.Edges {
		if previous, exists := edges[edge.ID]; exists {
			if !sameEdge(previous, edge) {
				return worldgraph.Chunk{}, fmt.Errorf("conflicting edge %+v in tile %+v", edge.ID, base.Tile)
			}
			edge.Sources = mergeSources(previous.Sources, edge.Sources)
		}
		edges[edge.ID] = edge
	}
	merged := worldgraph.Chunk{Tile: base.Tile}
	for _, node := range nodes {
		merged.Nodes = append(merged.Nodes, node)
	}
	for _, edge := range edges {
		merged.Edges = append(merged.Edges, edge)
	}
	return merged, nil
}

func normalizeChunk(chunk worldgraph.Chunk) worldgraph.Chunk {
	usedNodes := make(map[int64]struct{}, len(chunk.Edges)*2)
	for index := range chunk.Edges {
		chunk.Edges[index].Sources = mergeSources(chunk.Edges[index].Sources, nil)
		usedNodes[chunk.Edges[index].ID.From] = struct{}{}
		usedNodes[chunk.Edges[index].ID.To] = struct{}{}
	}
	keptNodes := chunk.Nodes[:0]
	for _, node := range chunk.Nodes {
		if _, used := usedNodes[node.ID]; used {
			keptNodes = append(keptNodes, node)
		}
	}
	chunk.Nodes = keptNodes
	sort.Slice(chunk.Nodes, func(i, j int) bool { return chunk.Nodes[i].ID < chunk.Nodes[j].ID })
	sort.Slice(chunk.Edges, func(i, j int) bool {
		left, right := chunk.Edges[i].ID, chunk.Edges[j].ID
		if left.WayID != right.WayID {
			return left.WayID < right.WayID
		}
		if left.From != right.From {
			return left.From < right.From
		}
		return left.To < right.To
	})
	return chunk
}

func tileSetSlice(set map[worldgraph.TileID]struct{}) []worldgraph.TileID {
	tiles := make([]worldgraph.TileID, 0, len(set))
	for tile := range set {
		tiles = append(tiles, tile)
	}
	sort.Slice(tiles, func(i, j int) bool {
		if tiles[i].Z != tiles[j].Z {
			return tiles[i].Z < tiles[j].Z
		}
		if tiles[i].X != tiles[j].X {
			return tiles[i].X < tiles[j].X
		}
		return tiles[i].Y < tiles[j].Y
	})
	return tiles
}
