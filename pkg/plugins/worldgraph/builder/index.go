package builder

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
	bolt "go.etcd.io/bbolt"
)

const (
	nodeKeySize   = 8
	nodeValueSize = 28
)

var nodeBucket = []byte("nodes")

type NodeIndex struct {
	db *bolt.DB
}

func OpenNodeIndex(path string) (*NodeIndex, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open node index: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(nodeBucket)
		return err
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize node index: %w", err)
	}
	return &NodeIndex{db: db}, nil
}

func (i *NodeIndex) PutBatch(ctx context.Context, nodes []worldgraph.Node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return i.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(nodeBucket)
		for _, node := range nodes {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := bucket.Put(nodeKey(node.ID), nodeValue(node)); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
}

func (i *NodeIndex) Get(id int64) (worldgraph.Node, bool, error) {
	var node worldgraph.Node
	found := false
	err := i.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(nodeBucket).Get(nodeKey(id))
		if value == nil {
			return nil
		}
		if len(value) != nodeValueSize {
			return fmt.Errorf("node %d has invalid value length %d", id, len(value))
		}
		node = decodeNodeValue(id, value)
		found = true
		return nil
	})
	return node, found, err
}

func (i *NodeIndex) Close() error {
	return i.db.Close()
}

func nodeKey(id int64) []byte {
	key := make([]byte, nodeKeySize)
	binary.BigEndian.PutUint64(key, uint64(id)^(uint64(1)<<63))
	return key
}

func decodeNodeKey(key []byte) int64 {
	return int64(binary.BigEndian.Uint64(key) ^ (uint64(1) << 63))
}

func nodeValue(node worldgraph.Node) []byte {
	value := make([]byte, nodeValueSize)
	binary.BigEndian.PutUint64(value[0:8], math.Float64bits(node.Lon))
	binary.BigEndian.PutUint64(value[8:16], math.Float64bits(node.Lat))
	binary.BigEndian.PutUint32(value[16:20], uint32(node.Owner.Z))
	binary.BigEndian.PutUint32(value[20:24], uint32(node.Owner.X))
	binary.BigEndian.PutUint32(value[24:28], uint32(node.Owner.Y))
	return value
}

func decodeNodeValue(id int64, value []byte) worldgraph.Node {
	return worldgraph.Node{
		ID:  id,
		Lon: math.Float64frombits(binary.BigEndian.Uint64(value[0:8])),
		Lat: math.Float64frombits(binary.BigEndian.Uint64(value[8:16])),
		Owner: worldgraph.TileID{
			Z: int(binary.BigEndian.Uint32(value[16:20])),
			X: int(binary.BigEndian.Uint32(value[20:24])),
			Y: int(binary.BigEndian.Uint32(value[24:28])),
		},
	}
}
