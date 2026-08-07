package builder

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
	bolt "go.etcd.io/bbolt"
)

var (
	contributionTiles        = []byte("tiles")
	contributionNodes        = []byte("nodes")
	contributionEdges        = []byte("edges")
	contributionEdgeBytesKey = []byte("edge-bytes")
	stagedChunks             = []byte("staged-chunks")
)

const (
	maxContributionEdgeValueBytes = 512 << 10
	maxContributionTileEdgeBytes  = 256 << 20
)

type edgeContribution struct {
	tile     worldgraph.TileID
	from, to worldgraph.Node
	edge     worldgraph.Edge
}

type contributionStore struct {
	db *bolt.DB
}

func openContributionStore(path string) (*contributionStore, error) {
	// Contribution databases are reconstructible, process-local scratch. The
	// immutable chunk/pack publication path supplies the required durability;
	// syncing every scratch transaction would add millions of HDD barriers.
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second, NoSync: true})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(contributionTiles); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(stagedChunks)
		return err
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &contributionStore{db: db}, nil
}

func (s *contributionStore) Close() error {
	return s.db.Close()
}

func (s *contributionStore) MarkTiles(ctx context.Context, tiles []worldgraph.TileID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		root := tx.Bucket(contributionTiles)
		for _, tile := range tiles {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := contributionTileBucket(root, tile); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
}

func (s *contributionStore) PutBatch(ctx context.Context, contributions []edgeContribution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		root := tx.Bucket(contributionTiles)
		for _, contribution := range contributions {
			if err := ctx.Err(); err != nil {
				return err
			}
			tileBucket, err := contributionTileBucket(root, contribution.tile)
			if err != nil {
				return err
			}
			nodes := tileBucket.Bucket(contributionNodes)
			if err := putContributionNode(nodes, contribution.from); err != nil {
				return err
			}
			if err := putContributionNode(nodes, contribution.to); err != nil {
				return err
			}
			edges := tileBucket.Bucket(contributionEdges)
			if err := putContributionEdge(tileBucket, edges, contribution.edge); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
}

func (s *contributionStore) Tiles(ctx context.Context) ([]worldgraph.TileID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var tiles []worldgraph.TileID
	err := s.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(contributionTiles).Cursor()
		for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			tiles = append(tiles, decodeTileKey(key))
		}
		return nil
	})
	return tiles, err
}

func (s *contributionStore) Chunk(ctx context.Context, tile worldgraph.TileID) (worldgraph.Chunk, bool, error) {
	if err := ctx.Err(); err != nil {
		return worldgraph.Chunk{}, false, err
	}
	chunk := worldgraph.Chunk{Tile: tile}
	found := false
	err := s.db.View(func(tx *bolt.Tx) error {
		tileBucket := tx.Bucket(contributionTiles).Bucket(tileKey(tile))
		if tileBucket == nil {
			return nil
		}
		found = true
		nodeCursor := tileBucket.Bucket(contributionNodes).Cursor()
		for key, value := nodeCursor.First(); key != nil; key, value = nodeCursor.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(chunk.Nodes) >= worldgraph.MaxChunkNodes {
				return fmt.Errorf("%w: tile %+v exceeds %d nodes", worldgraph.ErrInvalidChunk, tile, worldgraph.MaxChunkNodes)
			}
			if len(value) != nodeValueSize {
				return fmt.Errorf("contribution node has invalid value length %d", len(value))
			}
			chunk.Nodes = append(chunk.Nodes, decodeNodeValue(decodeNodeKey(key), value))
		}
		edgeCursor := tileBucket.Bucket(contributionEdges).Cursor()
		for _, value := edgeCursor.First(); value != nil; _, value = edgeCursor.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(chunk.Edges) >= worldgraph.MaxChunkEdges {
				return fmt.Errorf("%w: tile %+v exceeds %d edges", worldgraph.ErrInvalidChunk, tile, worldgraph.MaxChunkEdges)
			}
			edge, err := decodeContributionEdge(value)
			if err != nil {
				return err
			}
			chunk.Edges = append(chunk.Edges, edge)
		}
		return nil
	})
	return chunk, found, err
}

func (s *contributionStore) StageChunk(ctx context.Context, chunk worldgraph.Chunk) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(chunk); err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return tx.Bucket(stagedChunks).Put(tileKey(chunk.Tile), encoded.Bytes())
	})
}

func (s *contributionStore) StagedChunk(ctx context.Context, tile worldgraph.TileID) (worldgraph.Chunk, bool, error) {
	if err := ctx.Err(); err != nil {
		return worldgraph.Chunk{}, false, err
	}
	var chunk worldgraph.Chunk
	found := false
	err := s.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(stagedChunks).Get(tileKey(tile))
		if value == nil {
			return nil
		}
		found = true
		return gob.NewDecoder(bytes.NewReader(value)).Decode(&chunk)
	})
	return chunk, found, err
}

func contributionTileBucket(root *bolt.Bucket, tile worldgraph.TileID) (*bolt.Bucket, error) {
	bucket, err := root.CreateBucketIfNotExists(tileKey(tile))
	if err != nil {
		return nil, err
	}
	if _, err := bucket.CreateBucketIfNotExists(contributionNodes); err != nil {
		return nil, err
	}
	if _, err := bucket.CreateBucketIfNotExists(contributionEdges); err != nil {
		return nil, err
	}
	return bucket, nil
}

func putContributionNode(bucket *bolt.Bucket, node worldgraph.Node) error {
	key := nodeKey(node.ID)
	value := nodeValue(node)
	if previous := bucket.Get(key); previous != nil {
		if len(previous) != nodeValueSize || decodeNodeValue(node.ID, previous) != node {
			return fmt.Errorf("conflicting contribution node %d", node.ID)
		}
		return nil
	}
	count := bucket.Sequence()
	if count >= worldgraph.MaxChunkNodes {
		return fmt.Errorf("%w: tile exceeds %d contribution nodes", worldgraph.ErrInvalidChunk, worldgraph.MaxChunkNodes)
	}
	if err := bucket.Put(key, value); err != nil {
		return err
	}
	return bucket.SetSequence(count + 1)
}

func putContributionEdge(tileBucket, bucket *bolt.Bucket, edge worldgraph.Edge) error {
	key := contributionEdgeKey(edge.ID)
	previous := bucket.Get(key)
	previousBytes := len(previous)
	isNew := previous == nil
	if previous != nil {
		existing, err := decodeContributionEdge(previous)
		if err != nil {
			return err
		}
		if !sameEdge(existing, edge) {
			return fmt.Errorf("conflicting contribution edge %+v", edge.ID)
		}
		edge.Sources = mergeSources(existing.Sources, edge.Sources)
	}
	count := bucket.Sequence()
	if isNew && count >= worldgraph.MaxChunkEdges {
		return fmt.Errorf("%w: tile exceeds %d contribution edges", worldgraph.ErrInvalidChunk, worldgraph.MaxChunkEdges)
	}
	value, err := encodeContributionEdge(edge)
	if err != nil {
		return err
	}
	if len(value) > maxContributionEdgeValueBytes {
		return fmt.Errorf("%w: contribution edge exceeds %d bytes", worldgraph.ErrInvalidChunk, maxContributionEdgeValueBytes)
	}
	currentBytes := uint64(0)
	if encoded := tileBucket.Get(contributionEdgeBytesKey); encoded != nil {
		if len(encoded) != 8 {
			return fmt.Errorf("contribution edge byte count is invalid")
		}
		currentBytes = binary.BigEndian.Uint64(encoded)
	}
	if currentBytes < uint64(previousBytes) {
		return fmt.Errorf("contribution edge byte count underflows")
	}
	nextBytes := currentBytes - uint64(previousBytes) + uint64(len(value))
	if nextBytes > maxContributionTileEdgeBytes {
		return fmt.Errorf("%w: tile contributions exceed %d edge bytes", worldgraph.ErrInvalidChunk, maxContributionTileEdgeBytes)
	}
	if err := bucket.Put(key, value); err != nil {
		return err
	}
	var encodedBytes [8]byte
	binary.BigEndian.PutUint64(encodedBytes[:], nextBytes)
	if err := tileBucket.Put(contributionEdgeBytesKey, encodedBytes[:]); err != nil {
		return err
	}
	if isNew {
		return bucket.SetSequence(count + 1)
	}
	return nil
}

func encodeContributionEdge(edge worldgraph.Edge) ([]byte, error) {
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(edge); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func decodeContributionEdge(value []byte) (worldgraph.Edge, error) {
	var edge worldgraph.Edge
	if err := gob.NewDecoder(bytes.NewReader(value)).Decode(&edge); err != nil {
		return worldgraph.Edge{}, err
	}
	return edge, nil
}

func tileKey(tile worldgraph.TileID) []byte {
	key := make([]byte, 12)
	binary.BigEndian.PutUint32(key[0:4], uint32(tile.Z))
	binary.BigEndian.PutUint32(key[4:8], uint32(tile.X))
	binary.BigEndian.PutUint32(key[8:12], uint32(tile.Y))
	return key
}

func decodeTileKey(key []byte) worldgraph.TileID {
	return worldgraph.TileID{
		Z: int(binary.BigEndian.Uint32(key[0:4])),
		X: int(binary.BigEndian.Uint32(key[4:8])),
		Y: int(binary.BigEndian.Uint32(key[8:12])),
	}
}

func contributionEdgeKey(id worldgraph.EdgeID) []byte {
	key := make([]byte, 24)
	binary.BigEndian.PutUint64(key[0:8], uint64(id.WayID)^(uint64(1)<<63))
	binary.BigEndian.PutUint64(key[8:16], uint64(id.From)^(uint64(1)<<63))
	binary.BigEndian.PutUint64(key[16:24], uint64(id.To)^(uint64(1)<<63))
	return key
}

func sameEdge(left, right worldgraph.Edge) bool {
	return left.ID == right.ID &&
		left.DistanceMeters == right.DistanceMeters &&
		left.Highway == right.Highway &&
		left.Name == right.Name &&
		left.RestrictWalking == right.RestrictWalking &&
		left.RestrictDriving == right.RestrictDriving &&
		left.Owner == right.Owner
}

func mergeSources(left, right []string) []string {
	set := make(map[string]struct{}, len(left)+len(right))
	for _, source := range left {
		set[source] = struct{}{}
	}
	for _, source := range right {
		set[source] = struct{}{}
	}
	merged := make([]string, 0, len(set))
	for source := range set {
		merged = append(merged, source)
	}
	sort.Strings(merged)
	return merged
}
