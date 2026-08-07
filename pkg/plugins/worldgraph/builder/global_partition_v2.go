package builder

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	internalosm "github.com/danielscoffee/pathcraft/internal/osm"
	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

type globalFragmentCounts struct {
	Segments      int64
	Fragments     int64
	Contributions int64
}

func partitionGlobalFragments(
	ctx context.Context,
	waySpoolPath string,
	resolvedNodesPath string,
	spoolRoot string,
	maxOpen int,
	maxWayNodes int,
) ([]worldgraph.TileID, globalFragmentCounts, error) {
	if ctx == nil {
		return nil, globalFragmentCounts{}, fmt.Errorf("global fragment partition context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, globalFragmentCounts{}, err
	}
	if waySpoolPath == "" || resolvedNodesPath == "" || spoolRoot == "" || maxOpen < 1 {
		return nil, globalFragmentCounts{}, fmt.Errorf("global fragment partition paths and positive file limit are required")
	}
	resolved, err := openResolvedWayNodeReader(resolvedNodesPath)
	if err != nil {
		return nil, globalFragmentCounts{}, err
	}
	writer, err := newFragmentSpoolWriter(ctx, spoolRoot, maxOpen)
	if err != nil {
		_ = resolved.Close()
		return nil, globalFragmentCounts{}, err
	}
	var counts globalFragmentCounts
	replayErr := replayWaySpool(ctx, waySpoolPath, maxWayNodes, func(way Way) error {
		policy := internalosm.PolicyForTags(way.Tags)
		if !policy.Routable || len(way.NodeIDs) < 2 {
			return fmt.Errorf("way spool contains non-routable way %d", way.ID)
		}
		nodes := make([]worldgraph.Node, len(way.NodeIDs))
		for index, expectedID := range way.NodeIDs {
			if err := ctx.Err(); err != nil {
				return err
			}
			record, ok, err := resolved.Next()
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%w: resolved node stream ended inside way %d", ErrCorruptReferenceJoin, way.ID)
			}
			if record.Node.ID != expectedID {
				return fmt.Errorf("%w: way %d node %d resolved as %d", ErrCorruptReferenceJoin, way.ID, expectedID, record.Node.ID)
			}
			node := record.Node
			if validMercatorPosition(node.Lon, node.Lat) {
				owner, err := worldgraph.TileForPosition(node.Lon, node.Lat, worldgraph.GlobalRoutingZoom)
				if err != nil {
					return err
				}
				node.Owner = owner
			}
			nodes[index] = node
		}

		byShard := make(map[worldgraph.TileID][]globalFragmentSegment)
		for index := 0; index+1 < len(nodes); index++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if counts.Segments == math.MaxInt64 {
				return fmt.Errorf("global segment count exceeds int64")
			}
			counts.Segments++
			from, to := nodes[index], nodes[index+1]
			if from.ID == to.ID || !validMercatorPosition(from.Lon, from.Lat) || !validMercatorPosition(to.Lon, to.Lat) {
				continue
			}
			edges, err := segmentEdges(way, policy, from, to, fragmentPlanetSource, worldgraph.GlobalRoutingZoom)
			if err != nil {
				return err
			}
			if len(edges) != 2 {
				return fmt.Errorf("way %d segment expansion produced %d edges", way.ID, len(edges))
			}
			contributions := edgeContributions(from, to, edges[0])
			logicalContributions := int64(2 * len(contributions))
			if counts.Contributions > math.MaxInt64-logicalContributions {
				return fmt.Errorf("global contribution count exceeds int64")
			}
			counts.Contributions += logicalContributions
			segment := globalFragmentSegment{From: from, To: to}
			seenShards := make(map[worldgraph.TileID]struct{}, len(contributions))
			for _, contribution := range contributions {
				shard, err := packedBuilderShard(contribution.tile)
				if err != nil {
					return err
				}
				if _, exists := seenShards[shard]; exists {
					continue
				}
				seenShards[shard] = struct{}{}
				byShard[shard] = append(byShard[shard], segment)
			}
		}
		shards := make([]worldgraph.TileID, 0, len(byShard))
		for shard := range byShard {
			shards = append(shards, shard)
		}
		sort.Slice(shards, func(i, j int) bool {
			if shards[i].X != shards[j].X {
				return shards[i].X < shards[j].X
			}
			return shards[i].Y < shards[j].Y
		})
		for _, shard := range shards {
			fragment := globalWayFragment{
				WayID: way.ID, Highway: way.Tags["highway"], Name: way.Tags["name"],
				Direction: policy.Direction, RestrictWalking: policy.RestrictWalking,
				RestrictDriving: policy.RestrictDriving, Segments: byShard[shard],
			}
			if err := writer.Add(shard, fragment); err != nil {
				return err
			}
			if counts.Fragments == math.MaxInt64 {
				return fmt.Errorf("global fragment count exceeds int64")
			}
			counts.Fragments++
		}
		return nil
	})
	if replayErr == nil {
		if _, ok, err := resolved.Next(); err != nil {
			replayErr = err
		} else if ok {
			replayErr = fmt.Errorf("%w: resolved node stream has trailing occurrences", ErrCorruptReferenceJoin)
		}
	}
	resolvedCloseErr := resolved.Close()
	writerCloseErr := writer.Close()
	if replayErr != nil || resolvedCloseErr != nil || writerCloseErr != nil {
		return nil, globalFragmentCounts{}, errors.Join(replayErr, resolvedCloseErr, writerCloseErr)
	}
	return writer.Shards(), counts, nil
}
