package graph

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestCacheMetadataAndContractionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "map.osm")
	if err := os.WriteFile(source, []byte("source-v1"), 0o600); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}
	fingerprint, err := FingerprintFile(source)
	if err != nil {
		t.Fatalf("FingerprintFile() error = %v", err)
	}

	g := NewGraph()
	for id := NodeID(1); id <= 4; id++ {
		g.AddNode(id, 0, float64(id))
		if id > 1 {
			g.AddBidirectionalEdge(id-1, id, 10)
		}
	}
	g.Contraction, _ = BuildDegreeTwoContraction(g)

	cachePath := filepath.Join(dir, "graph.cache")
	if err := g.SaveCache(cachePath, CacheMetadata{SourceSHA256: fingerprint}); err != nil {
		t.Fatalf("SaveCache() error = %v", err)
	}

	metadata, err := ReadCacheMetadata(cachePath)
	if err != nil {
		t.Fatalf("ReadCacheMetadata() error = %v", err)
	}
	if metadata.SourceSHA256 != fingerprint || metadata.GraphVersion != CacheVersion || metadata.PreprocessVersion != PreprocessVersion {
		t.Fatalf("metadata = %+v", metadata)
	}

	loaded, loadedMetadata, err := LoadGraphCache(cachePath)
	if err != nil {
		t.Fatalf("LoadGraphCache() error = %v", err)
	}
	if loadedMetadata != metadata {
		t.Fatalf("loaded metadata = %+v, want %+v", loadedMetadata, metadata)
	}
	if loaded.nearestNodeIndex == nil {
		t.Fatal("loaded graph did not publish rebuilt spatial index")
	}
	if !reflect.DeepEqual(loaded.Nodes, g.Nodes) {
		t.Fatalf("loaded nodes = %+v, want %+v", loaded.Nodes, g.Nodes)
	}
	if !reflect.DeepEqual(loaded.Edges, g.Edges) {
		t.Fatalf("loaded edges = %+v, want %+v", loaded.Edges, g.Edges)
	}
	assertContractionEqual(t, loaded.Contraction, g.Contraction)
}

func assertContractionEqual(t *testing.T, got, want *ContractionIndex) {
	t.Helper()
	if got == nil || want == nil || got.Version != want.Version ||
		!reflect.DeepEqual(got.Retained, want.Retained) ||
		!reflect.DeepEqual(got.Out, want.Out) ||
		!reflect.DeepEqual(got.Positions, want.Positions) ||
		len(got.Chains) != len(want.Chains) {
		t.Fatalf("loaded contraction = %+v, want %+v", got, want)
	}
	for i := range want.Chains {
		gotChain, wantChain := got.Chains[i], want.Chains[i]
		if !slices.Equal(gotChain.Nodes, wantChain.Nodes) ||
			gotChain.DistanceM != wantChain.DistanceM ||
			!reflect.DeepEqual(gotChain.CostComponents, wantChain.CostComponents) ||
			!slices.Equal(gotChain.RestrictedModes, wantChain.RestrictedModes) ||
			len(gotChain.Segments) != len(wantChain.Segments) {
			t.Fatalf("loaded chain %d = %+v, want %+v", i, gotChain, wantChain)
		}
		for j := range wantChain.Segments {
			gotEdge, wantEdge := gotChain.Segments[j], wantChain.Segments[j]
			if gotEdge.To != wantEdge.To || gotEdge.Cost != wantEdge.Cost || gotEdge.DistanceM != wantEdge.DistanceM ||
				gotEdge.Highway != wantEdge.Highway || gotEdge.Name != wantEdge.Name ||
				!slices.Equal(gotEdge.RestrictedModes, wantEdge.RestrictedModes) {
				t.Fatalf("loaded chain %d segment %d = %+v, want %+v", i, j, gotEdge, wantEdge)
			}
		}
	}
}

func TestFingerprintFileChangesWithContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.osm")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatalf("WriteFile(first) error = %v", err)
	}
	first, err := FingerprintFile(path)
	if err != nil {
		t.Fatalf("FingerprintFile(first) error = %v", err)
	}
	if err := os.WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatalf("WriteFile(second) error = %v", err)
	}
	second, err := FingerprintFile(path)
	if err != nil {
		t.Fatalf("FingerprintFile(second) error = %v", err)
	}
	if first == second {
		t.Fatal("fingerprint did not change with source content")
	}
}

func TestReadCacheMetadataDoesNotDecodePayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.cache")
	g := NewGraph()
	g.AddNode(1, 0, 0)
	if err := g.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := os.Truncate(path, cacheHeaderSize); err != nil {
		t.Fatalf("Truncate() error = %v", err)
	}

	if _, err := ReadCacheMetadata(path); err != nil {
		t.Fatalf("ReadCacheMetadata() decoded payload: %v", err)
	}
	if _, err := LoadGraph(path); err == nil {
		t.Fatal("LoadGraph() accepted missing payload")
	}
}

func TestLoadGraphRejectsCacheVersionMismatch(t *testing.T) {
	tests := []struct {
		name   string
		offset int64
	}{
		{name: "graph", offset: cacheGraphVersionOffset},
		{name: "preprocess", offset: cachePreprocessVersionOffset},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "graph.cache")
			g := NewGraph()
			g.AddNode(1, 0, 0)
			if err := g.Save(path); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			f, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				t.Fatalf("OpenFile() error = %v", err)
			}
			var encoded [4]byte
			binary.BigEndian.PutUint32(encoded[:], 999)
			if _, err := f.WriteAt(encoded[:], test.offset); err != nil {
				t.Fatalf("WriteAt() error = %v", err)
			}
			if err := f.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if _, err := LoadGraph(path); err == nil {
				t.Fatal("LoadGraph() accepted mismatched cache version")
			}
		})
	}
}

func TestLoadGraphRejectsMalformedContractionReferences(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ContractionIndex)
	}{
		{name: "chain ID", mutate: func(index *ContractionIndex) { index.Out[1] = []int{len(index.Chains)} }},
		{name: "position offset", mutate: func(index *ContractionIndex) { index.Positions[1][0].Offset = 99 }},
		{name: "segment count", mutate: func(index *ContractionIndex) { index.Chains[0].Segments = nil }},
		{name: "missing node", mutate: func(index *ContractionIndex) { index.Chains[0].Nodes[1] = 99 }},
		{name: "numeric field", mutate: func(index *ContractionIndex) { index.Chains[0].Segments[0].DistanceM = math.NaN() }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "graph.cache")
			g := NewGraph()
			g.AddNode(1, 0, 0)
			g.AddNode(2, 0, 1)
			g.AddBidirectionalEdge(1, 2, 1)
			g.Contraction, _ = BuildDegreeTwoContraction(g)
			test.mutate(g.Contraction)
			if err := g.Save(path); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			if _, err := LoadGraph(path); err == nil {
				t.Fatal("LoadGraph() accepted malformed contraction")
			}
		})
	}
}

func TestLoadGraphRejectsContractionVersionMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.cache")
	g := NewGraph()
	g.AddNode(1, 0, 0)
	g.Contraction = &ContractionIndex{Version: 999}
	if err := g.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := LoadGraph(path); err == nil {
		t.Fatal("LoadGraph() accepted mismatched contraction version")
	}
}

func TestSaveCacheFailurePreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.cache")
	original := NewGraph()
	original.AddNode(1, 0, 0)
	if err := original.Save(path); err != nil {
		t.Fatalf("original Save() error = %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(before) error = %v", err)
	}

	replacement := NewGraph()
	replacement.AddNode(2, 0, 0)
	renameErr := errors.New("forced rename failure")
	if err := replacement.saveCache(path, CacheMetadata{}, func(_, _ string) error { return renameErr }); !errors.Is(err, renameErr) {
		t.Fatalf("saveCache() error = %v, want %v", err, renameErr)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(after) error = %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("failed cache write changed existing cache")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("cache directory entries = %v, want only preserved cache", entries)
	}
}

func TestSaveCacheUsesSecureModeAndPreservesExistingMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.cache")
	g := NewGraph()
	g.AddNode(1, 0, 0)
	if err := g.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(new cache) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("new cache mode = %o, want 600", got)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	if err := g.Save(path); err != nil {
		t.Fatalf("replacement Save() error = %v", err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(replacement cache) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("replacement cache mode = %o, want preserved 640", got)
	}
}

func TestSaveCacheAtomicallyReplacesFileWithoutTempLeak(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.cache")
	first := NewGraph()
	first.AddNode(1, 0, 0)
	if err := first.Save(path); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}

	second := NewGraph()
	second.AddNode(2, 0, 0)
	if err := second.Save(path); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
	loaded, err := LoadGraph(path)
	if err != nil {
		t.Fatalf("LoadGraph() error = %v", err)
	}
	if !loaded.HasNode(2) || loaded.HasNode(1) {
		t.Fatalf("loaded nodes = %v, want replacement graph", loaded.Nodes)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("cache directory entries = %v, want only final cache", entries)
	}
}
