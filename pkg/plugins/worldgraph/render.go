package worldgraph

import (
	"context"
	"encoding/json"
	"fmt"
)

type chunkFeatureCollection struct {
	Type     string         `json:"type"`
	Features []chunkFeature `json:"features"`
}

type chunkFeature struct {
	Type       string                 `json:"type"`
	Geometry   chunkLineString        `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}

type chunkLineString struct {
	Type        string       `json:"type"`
	Coordinates [][2]float64 `json:"coordinates"`
}

func (r *Router) ChunkGeoJSON(ctx context.Context, z, x, y int) ([]byte, error) {
	if err := r.checkContext(ctx); err != nil {
		return nil, err
	}
	tile := TileID{Z: z, X: x, Y: y}
	n, err := tileCount(z)
	if err == nil {
		err = validateTile(tile, n)
	}
	if err != nil || z != r.manifest.Zoom {
		if err == nil {
			err = fmt.Errorf("tile zoom %d does not match routing zoom %d", z, r.manifest.Zoom)
		}
		return nil, err
	}
	if !r.isCovered(tile) {
		return nil, fmt.Errorf("%w: %+v", ErrUncoveredTile, tile)
	}
	chunk, err := r.loadChunk(ctx, tile)
	if err != nil {
		return nil, err
	}
	nodes := make(map[int64]Node, len(chunk.Nodes))
	for _, node := range chunk.Nodes {
		nodes[node.ID] = node
	}
	collection := chunkFeatureCollection{Type: "FeatureCollection", Features: make([]chunkFeature, 0)}
	for _, edge := range chunk.Edges {
		if edge.Owner != tile {
			continue
		}
		from, fromOK := nodes[edge.ID.From]
		to, toOK := nodes[edge.ID.To]
		if !fromOK || !toOK {
			return nil, fmt.Errorf("%w: edge %+v has missing endpoint", ErrCorruptChunk, edge.ID)
		}
		collection.Features = append(collection.Features, chunkFeature{
			Type: "Feature",
			Geometry: chunkLineString{Type: "LineString", Coordinates: [][2]float64{
				{from.Lon, from.Lat}, {to.Lon, to.Lat},
			}},
			Properties: map[string]interface{}{
				"way_id": edge.ID.WayID, "from": edge.ID.From, "to": edge.ID.To,
				"highway": edge.Highway, "name": edge.Name,
				"restrict_walking": edge.RestrictWalking, "restrict_driving": edge.RestrictDriving,
			},
		})
	}
	return json.Marshal(collection)
}
