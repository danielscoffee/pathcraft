package engine

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/danielscoffee/pathcraft/internal/graph"
)

func TestLoadOSMPreprocessesAndCacheRoundTrips(t *testing.T) {
	const source = "../../../testdata/example.osm"
	e := New()
	if err := e.LoadOSM(source); err != nil {
		t.Fatalf("LoadOSM() error = %v", err)
	}
	if e.graph.Contraction == nil {
		t.Fatal("LoadOSM() did not publish contraction index")
	}
	stats := e.Stats()
	if stats.ContractedNodes != 4 || stats.ContractionChains != 2 {
		t.Fatalf("Stats() = %+v, want 4 contracted nodes and 2 directed chains", stats)
	}

	wantFingerprint, err := graph.FingerprintFile(source)
	if err != nil {
		t.Fatalf("FingerprintFile() error = %v", err)
	}
	if e.graphSourceSHA256 != wantFingerprint {
		t.Fatalf("engine fingerprint = %x, want %x", e.graphSourceSHA256, wantFingerprint)
	}

	cachePath := filepath.Join(t.TempDir(), "example.cache")
	if err := e.SaveGraph(cachePath); err != nil {
		t.Fatalf("SaveGraph() error = %v", err)
	}
	metadata, err := graph.ReadCacheMetadata(cachePath)
	if err != nil {
		t.Fatalf("ReadCacheMetadata() error = %v", err)
	}
	if metadata.SourceSHA256 != wantFingerprint {
		t.Fatalf("cache fingerprint = %x, want %x", metadata.SourceSHA256, wantFingerprint)
	}

	loaded := New()
	if err := loaded.LoadGraph(cachePath); err != nil {
		t.Fatalf("LoadGraph() error = %v", err)
	}
	if loaded.graphSourceSHA256 != wantFingerprint || loaded.Stats() != stats {
		t.Fatalf("loaded engine fingerprint/stats = %x %+v, want %x %+v", loaded.graphSourceSHA256, loaded.Stats(), wantFingerprint, stats)
	}
	route, err := loaded.Route(RouteRequest{From: 1, To: 6})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if !slices.Equal(route.Nodes, []int64{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("route nodes = %v, want full original path", route.Nodes)
	}
}
