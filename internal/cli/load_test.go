package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func TestLoadEngineRebuildsCacheWhenSourceChanges(t *testing.T) {
	source := copyOSMFixture(t)
	firstEngine, err := loadEngine(source)
	if err != nil {
		t.Fatalf("first loadEngine() error = %v", err)
	}
	if firstEngine.Stats().ContractedNodes == 0 {
		t.Fatal("first load did not preprocess contraction")
	}
	cachePath := source + ".cache"
	firstMetadata, err := graph.ReadCacheMetadata(cachePath)
	if err != nil {
		t.Fatalf("ReadCacheMetadata(first) error = %v", err)
	}

	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("ReadFile(source) error = %v", err)
	}
	changed := strings.Replace(string(contents), "</osm>", `  <node id="999" lat="0" lon="0"/>
</osm>`, 1)
	if err := os.WriteFile(source, []byte(changed), 0o600); err != nil {
		t.Fatalf("WriteFile(changed source) error = %v", err)
	}

	secondEngine, err := loadEngine(source)
	if err != nil {
		t.Fatalf("second loadEngine() error = %v", err)
	}
	secondMetadata, err := graph.ReadCacheMetadata(cachePath)
	if err != nil {
		t.Fatalf("ReadCacheMetadata(second) error = %v", err)
	}
	wantFingerprint, err := graph.FingerprintFile(source)
	if err != nil {
		t.Fatalf("FingerprintFile() error = %v", err)
	}
	if secondMetadata.SourceSHA256 == firstMetadata.SourceSHA256 || secondMetadata.SourceSHA256 != wantFingerprint {
		t.Fatalf("cache fingerprints: first=%x second=%x want=%x", firstMetadata.SourceSHA256, secondMetadata.SourceSHA256, wantFingerprint)
	}
	if secondEngine.Stats().Nodes != firstEngine.Stats().Nodes {
		t.Fatalf("unreferenced source node changed graph stats: first=%+v second=%+v", firstEngine.Stats(), secondEngine.Stats())
	}
}

func TestCmdPreprocessWritesRequestedCache(t *testing.T) {
	source := copyOSMFixture(t)
	output := filepath.Join(t.TempDir(), "prepared.cache")
	if err := CmdPreprocess([]string{"--file", source, "--output", output}); err != nil {
		t.Fatalf("CmdPreprocess() error = %v", err)
	}

	metadata, err := graph.ReadCacheMetadata(output)
	if err != nil {
		t.Fatalf("ReadCacheMetadata() error = %v", err)
	}
	wantFingerprint, err := graph.FingerprintFile(source)
	if err != nil {
		t.Fatalf("FingerprintFile() error = %v", err)
	}
	if metadata.SourceSHA256 != wantFingerprint {
		t.Fatalf("cache fingerprint = %x, want %x", metadata.SourceSHA256, wantFingerprint)
	}
	loaded := engine.New()
	if err := loaded.LoadGraph(output); err != nil {
		t.Fatalf("LoadGraph() error = %v", err)
	}
	if loaded.Stats().ContractedNodes == 0 {
		t.Fatalf("preprocessed stats = %+v, want contraction", loaded.Stats())
	}
}

func copyOSMFixture(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile("../../testdata/example.osm")
	if err != nil {
		t.Fatalf("ReadFile(fixture) error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "map.osm")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile(fixture) error = %v", err)
	}
	return path
}
