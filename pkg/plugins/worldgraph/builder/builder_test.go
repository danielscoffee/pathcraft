package builder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

func TestBuildAssignsOneOwnerAndNeighborHalos(t *testing.T) {
	storePath := t.TempDir()
	manifest := buildFixture(t, context.Background(), storePath, "region-a")
	if len(manifest.Tiles) != 2 || len(manifest.Regions) != 1 {
		t.Fatalf("manifest = %+v, want two seam tiles and one region", manifest)
	}

	store := openBuiltStore(t, storePath)
	id := worldgraph.EdgeID{WayID: 100, From: 1, To: 2}
	occurrences, ownerOccurrences := 0, 0
	for _, tile := range manifest.Tiles {
		chunk := loadBuiltChunk(t, store, tile)
		for _, edge := range chunk.Edges {
			if edge.ID != id {
				continue
			}
			occurrences++
			if edge.Owner == chunk.Tile {
				ownerOccurrences++
			}
			if !chunkHasNode(chunk, edge.ID.From) || !chunkHasNode(chunk, edge.ID.To) {
				t.Fatalf("halo chunk %+v lacks edge endpoints: %+v", chunk.Tile, chunk)
			}
		}
	}
	if occurrences != 2 || ownerOccurrences != 1 {
		t.Fatalf("edge occurrences = %d, owned occurrences = %d, want 2 and 1", occurrences, ownerOccurrences)
	}
}

func TestBuildMergesOverlappingRegionsByStableEdgeID(t *testing.T) {
	storePath := t.TempDir()
	buildFixture(t, context.Background(), storePath, "region-a")
	manifest := buildFixture(t, context.Background(), storePath, "region-b")
	if len(manifest.Regions) != 2 {
		t.Fatalf("regions = %+v, want two", manifest.Regions)
	}

	store := openBuiltStore(t, storePath)
	id := worldgraph.EdgeID{WayID: 100, From: 1, To: 2}
	for _, tile := range manifest.Tiles {
		chunk := loadBuiltChunk(t, store, tile)
		count := 0
		for _, edge := range chunk.Edges {
			if edge.ID == id {
				count++
				if !reflect.DeepEqual(edge.Sources, []string{"region-a", "region-b"}) {
					t.Fatalf("edge sources = %v, want both regions", edge.Sources)
				}
			}
		}
		if count != 1 {
			t.Fatalf("tile %+v edge count = %d, want one stable edge", tile, count)
		}
	}
}

func TestReplacingRegionRemovesDeletedEdges(t *testing.T) {
	storePath := t.TempDir()
	buildFixture(t, context.Background(), storePath, "region-a")
	manifest := buildFixtureWithoutWays(t, storePath, "region-a")

	store := openBuiltStore(t, storePath)
	for _, tile := range manifest.Tiles {
		if chunk := loadBuiltChunk(t, store, tile); len(chunk.Edges) != 0 {
			t.Fatalf("replacement tile %+v retained deleted edges: %+v", tile, chunk.Edges)
		}
	}
}

func TestReplacingRegionPreservesSharedBorderEdges(t *testing.T) {
	storePath := t.TempDir()
	buildFixture(t, context.Background(), storePath, "region-a")
	buildFixture(t, context.Background(), storePath, "region-b")
	manifest := buildFixtureWithoutWays(t, storePath, "region-a")

	store := openBuiltStore(t, storePath)
	id := worldgraph.EdgeID{WayID: 100, From: 1, To: 2}
	found := 0
	for _, tile := range manifest.Tiles {
		chunk := loadBuiltChunk(t, store, tile)
		for _, edge := range chunk.Edges {
			if edge.ID == id {
				found++
				if !reflect.DeepEqual(edge.Sources, []string{"region-b"}) {
					t.Fatalf("shared edge sources = %v, want region-b", edge.Sources)
				}
			}
		}
	}
	if found != 2 {
		t.Fatalf("shared edge occurrences = %d, want two halo copies", found)
	}
}

func TestCancelledBuildLeavesCurrentGenerationUntouched(t *testing.T) {
	storePath := t.TempDir()
	before := buildFixture(t, context.Background(), storePath, "region-a")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Build(ctx, fixtureOptions(t, storePath, "region-a"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Build() error = %v, want context.Canceled", err)
	}
	after := currentManifest(t, storePath)
	if after.Generation != before.Generation {
		t.Fatalf("generation changed after cancellation: %q -> %q", before.Generation, after.Generation)
	}
}

func TestMalformedPBFLeavesCurrentGenerationUntouched(t *testing.T) {
	storePath := t.TempDir()
	before := buildFixture(t, context.Background(), storePath, "region-a")
	malformed := filepath.Join(t.TempDir(), "malformed.osm.pbf")
	if err := os.WriteFile(malformed, []byte("not a PBF"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := fixtureOptions(t, storePath, "region-a")
	options.PBFPath = malformed
	if _, err := Build(context.Background(), options); err == nil {
		t.Fatal("Build() error = nil, want malformed PBF rejection")
	}
	after := currentManifest(t, storePath)
	if after.Generation != before.Generation {
		t.Fatalf("generation changed after malformed input: %q -> %q", before.Generation, after.Generation)
	}
}

func buildFixture(t *testing.T, ctx context.Context, storePath, region string) worldgraph.Manifest {
	t.Helper()
	manifest, err := Build(ctx, fixtureOptions(t, storePath, region))
	if err != nil {
		t.Fatalf("Build(%s) error = %v", region, err)
	}
	return manifest
}

func buildFixtureWithoutWays(t *testing.T, storePath, region string) worldgraph.Manifest {
	t.Helper()
	functions := scanFunctions{
		nodes: ScanNodes,
		ways: func(context.Context, string, int, func(Way) error) error {
			return nil
		},
	}
	manifest, err := build(context.Background(), fixtureOptions(t, storePath, region), functions)
	if err != nil {
		t.Fatalf("buildWithoutWays(%s) error = %v", region, err)
	}
	return manifest
}

func fixtureOptions(t *testing.T, storePath, region string) Options {
	t.Helper()
	return Options{
		PBFPath:   seamFixturePath(),
		StorePath: storePath,
		Region:    region,
		Zoom:      worldgraph.DefaultZoom,
		TempDir:   t.TempDir(),
	}
}

func openBuiltStore(t *testing.T, path string) *worldgraph.Store {
	t.Helper()
	store, err := worldgraph.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func loadBuiltChunk(t *testing.T, store *worldgraph.Store, tile worldgraph.TileID) *worldgraph.Chunk {
	t.Helper()
	chunk, err := store.LoadChunk(tile)
	if err != nil {
		t.Fatal(err)
	}
	return chunk
}

func currentManifest(t *testing.T, storePath string) worldgraph.Manifest {
	t.Helper()
	manifest, err := openBuiltStore(t, storePath).Manifest()
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func chunkHasNode(chunk *worldgraph.Chunk, id int64) bool {
	for _, node := range chunk.Nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}
