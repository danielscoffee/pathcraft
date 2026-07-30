package builder

import (
	"encoding/binary"
	"errors"
	"path/filepath"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
	bolt "go.etcd.io/bbolt"
)

func TestContributionBucketsRejectStructuralLimits(t *testing.T) {
	db, err := bolt.Open(filepath.Join(t.TempDir(), "limits.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		nodes, err := tx.CreateBucket([]byte("nodes"))
		if err != nil {
			return err
		}
		if err := nodes.SetSequence(worldgraph.MaxChunkNodes); err != nil {
			return err
		}
		if err := putContributionNode(nodes, worldgraph.Node{ID: 1}); !errors.Is(err, worldgraph.ErrInvalidChunk) {
			t.Fatalf("putContributionNode() error = %v, want ErrInvalidChunk", err)
		}

		tile, err := tx.CreateBucket([]byte("tile"))
		if err != nil {
			return err
		}
		edges, err := tile.CreateBucket([]byte("edges"))
		if err != nil {
			return err
		}
		if err := edges.SetSequence(worldgraph.MaxChunkEdges); err != nil {
			return err
		}
		edge := worldgraph.Edge{ID: worldgraph.EdgeID{WayID: 1, From: 1, To: 2}}
		if err := putContributionEdge(tile, edges, edge); !errors.Is(err, worldgraph.ErrInvalidChunk) {
			t.Fatalf("putContributionEdge() error = %v, want ErrInvalidChunk", err)
		}

		budgetTile, err := tx.CreateBucket([]byte("budget-tile"))
		if err != nil {
			return err
		}
		budgetEdges, err := budgetTile.CreateBucket([]byte("edges"))
		if err != nil {
			return err
		}
		var encodedBytes [8]byte
		binary.BigEndian.PutUint64(encodedBytes[:], maxContributionTileEdgeBytes)
		if err := budgetTile.Put(contributionEdgeBytesKey, encodedBytes[:]); err != nil {
			return err
		}
		if err := putContributionEdge(budgetTile, budgetEdges, edge); !errors.Is(err, worldgraph.ErrInvalidChunk) {
			t.Fatalf("putContributionEdge() budget error = %v, want ErrInvalidChunk", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
