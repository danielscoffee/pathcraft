package worldgraph

import (
	"context"
	"fmt"
	"strconv"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

type Loader struct{}

func (Loader) Name() string { return "worldgraph" }

func (Loader) Load(ctx context.Context, source string) (core.Graph, error) {
	if source == "" {
		return nil, fmt.Errorf("worldgraph loader: source path is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	router, err := OpenRouter(source, RouterOptions{})
	if err != nil {
		return nil, fmt.Errorf("worldgraph loader: %w", err)
	}
	return router, nil
}

func (r *Router) Neighbors(ctx context.Context, nodeID core.NodeID) ([]core.Edge, error) {
	if err := r.checkContext(ctx); err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(string(nodeID), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("worldgraph node id %q is not an OSM integer", nodeID)
	}

	r.graphMu.Lock()
	defer r.graphMu.Unlock()
	owner, found := r.nodeOwners[id]
	for !found && r.scanIndex < len(r.manifest.Tiles) {
		chunk, err := r.loadChunk(ctx, r.manifest.Tiles[r.scanIndex])
		if err != nil {
			return nil, err
		}
		r.scanIndex++
		if err := r.indexChunkNodesLocked(chunk); err != nil {
			return nil, err
		}
		owner, found = r.nodeOwners[id]
	}
	if !found {
		return nil, fmt.Errorf("worldgraph node %d not found", id)
	}
	chunk, err := r.loadChunk(ctx, owner)
	if err != nil {
		return nil, err
	}
	if err := r.indexChunkNodesLocked(chunk); err != nil {
		return nil, err
	}
	edges := make([]core.Edge, 0)
	for _, edge := range chunk.Edges {
		if edge.ID.From != id {
			continue
		}
		edges = append(edges, core.Edge{
			From: nodeID,
			To:   core.NodeID(strconv.FormatInt(edge.ID.To, 10)),
			Cost: edge.DistanceMeters,
			Meta: map[string]any{
				"way_id": edge.ID.WayID, "highway": edge.Highway, "name": edge.Name,
				"restrict_walking": edge.RestrictWalking, "restrict_driving": edge.RestrictDriving,
			},
		})
	}
	return edges, nil
}

func (r *Router) indexChunkNodes(chunk *Chunk) error {
	r.graphMu.Lock()
	defer r.graphMu.Unlock()
	return r.indexChunkNodesLocked(chunk)
}

func (r *Router) indexChunkNodesLocked(chunk *Chunk) error {
	for _, node := range chunk.Nodes {
		if owner, exists := r.nodeOwners[node.ID]; exists && owner != node.Owner {
			return fmt.Errorf("%w: conflicting owner for node %d", ErrCorruptChunk, node.ID)
		}
		r.nodeOwners[node.ID] = node.Owner
	}
	return nil
}

func init() { plugins.MustRegisterLoader(Loader{}) }

var _ core.GraphLoader = Loader{}
var _ core.Graph = (*Router)(nil)
