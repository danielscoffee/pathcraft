package builder

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

func TestPBFSourceSupportsStableRepeatedPasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.osm.pbf")
	data, err := os.ReadFile(seamFixturePath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := openPBFSource(context.Background(), path, DefaultMaxWayNodes)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if source.Size() != int64(len(data)) || source.Fingerprint() != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("source identity = %d, %s", source.Size(), source.Fingerprint())
	}

	var first, second []int64
	if err := scanNodesReader(context.Background(), source.Reader(), func(nodes []worldgraph.Node) error {
		for _, node := range nodes {
			first = append(first, node.ID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := scanNodesReader(context.Background(), source.Reader(), func(nodes []worldgraph.Node) error {
		for _, node := range nodes {
			second = append(second, node.ID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, []int64{1, 2, 3}) || !reflect.DeepEqual(second, first) {
		t.Fatalf("node passes = %v, %v", first, second)
	}
	var ways []Way
	if err := scanWaysReader(context.Background(), source.Reader(), DefaultMaxWayNodes, func(way Way) error {
		ways = append(ways, way)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(ways) != 2 {
		t.Fatalf("way pass count = %d, want 2", len(ways))
	}
	if err := source.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPBFSourceDescriptorSurvivesPathReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.osm.pbf")
	data, err := os.ReadFile(seamFixturePath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := openPBFSource(context.Background(), path, DefaultMaxWayNodes)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if err := os.Rename(path, filepath.Join(root, "original.osm.pbf")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	var nodes int
	if err := scanNodesReader(context.Background(), source.Reader(), func(batch []worldgraph.Node) error {
		nodes += len(batch)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if nodes != 3 {
		t.Fatalf("descriptor node count = %d, want 3", nodes)
	}
	if err := source.Verify(context.Background()); err != nil {
		t.Fatalf("Verify() after path replacement = %v", err)
	}
}

func TestPBFSourceDetectsInPlaceRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.osm.pbf")
	data, err := os.ReadFile(seamFixturePath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := openPBFSource(context.Background(), path, DefaultMaxWayNodes)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	mutated := append([]byte(nil), data...)
	mutated[len(mutated)-1] ^= 0xff
	if err := os.WriteFile(path, mutated, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := source.Verify(context.Background()); !errors.Is(err, ErrPBFSourceChanged) {
		t.Fatalf("Verify() error = %v, want ErrPBFSourceChanged", err)
	}
}

func TestPBFSourceRejectsNonRegularAndHonorsCancellation(t *testing.T) {
	if _, err := openPBFSource(context.Background(), t.TempDir(), DefaultMaxWayNodes); err == nil {
		t.Fatal("openPBFSource(directory) error = nil")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := openPBFSource(ctx, seamFixturePath(), DefaultMaxWayNodes); !errors.Is(err, context.Canceled) {
		t.Fatalf("openPBFSource(canceled) error = %v, want context.Canceled", err)
	}
}

func TestPBFSourceCloseReleasesDescriptor(t *testing.T) {
	source, err := openPBFSource(context.Background(), seamFixturePath(), DefaultMaxWayNodes)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1)
	if _, err := source.Reader().Read(buffer); !errors.Is(err, os.ErrClosed) && !errors.Is(err, io.EOF) {
		t.Fatalf("read after Close() error = %v", err)
	}
}
