package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

func TestCmdChunksBuildsGenerationAndPrintsSummary(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "world")
	fixture := filepath.Join("..", "..", "pkg", "plugins", "worldgraph", "builder", "testdata", "seam.osm.pbf")
	output, err := captureCLIStdout(t, func() error {
		return CmdChunks([]string{"build", "--pbf", fixture, "--store", storePath, "--region", "cli-test", "--zoom", "12"})
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := worldgraph.OpenStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := store.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Generation == "" || len(manifest.Tiles) == 0 || len(manifest.Regions) != 1 || manifest.Regions[0].Name != "cli-test" {
		t.Fatalf("manifest = %+v", manifest)
	}
	for _, want := range []string{manifest.Generation, "Tiles:", "cli-test"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output %q lacks %q", output, want)
		}
	}
}

func TestCmdChunksBuildGlobalPublishesAndResumes(t *testing.T) {
	root := t.TempDir()
	storePath := filepath.Join(root, "world")
	workDir := filepath.Join(root, "work")
	fixture := filepath.Join("..", "..", "pkg", "plugins", "worldgraph", "builder", "testdata", "seam.osm.pbf")
	args := []string{
		"build-global", "--pbf", fixture, "--store", storePath, "--work-dir", workDir,
		"--run-memory-mb", "1", "--pack-mb", "1", "--open-shards", "2",
	}
	output, err := captureCLIStdout(t, func() error { return CmdChunks(args) })
	if err != nil {
		t.Fatal(err)
	}
	store, err := worldgraph.OpenStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := store.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	if manifest.Layout != worldgraph.PackedLayout || len(manifest.Shards) == 0 || len(manifest.Tiles) != 0 {
		t.Fatalf("manifest = %+v", manifest)
	}
	for _, want := range []string{"Stage:", "Generation: " + manifest.Generation, "Shards:", "Chunks:", "Nodes:", "Edges:", "Work bytes:", "Store bytes:", "Elapsed:"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output %q lacks %q", output, want)
		}
	}
	resumed, err := captureCLIStdout(t, func() error { return CmdChunks(args) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resumed, manifest.Generation) {
		t.Fatalf("resumed output %q lacks generation %q", resumed, manifest.Generation)
	}
	noResume := append(append([]string(nil), args...), "--resume=false")
	if _, err := captureCLIStdout(t, func() error { return CmdChunks(noResume) }); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("disabled resume error = %v", err)
	}
}

func TestCmdChunksBuildGlobalHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := CmdChunksContext(ctx, []string{"build-global", "--pbf", "unused.osm.pbf", "--store", filepath.Join(t.TempDir(), "store"), "--work-dir", filepath.Join(t.TempDir(), "work")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CmdChunksContext() error = %v, want context.Canceled", err)
	}
}

func TestCmdChunksBuildGlobalRejectsInvalidFlags(t *testing.T) {
	base := []string{"build-global", "--pbf", "x", "--store", "y", "--work-dir", "z"}
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"build-global"}, "--pbf is required"},
		{[]string{"build-global", "--pbf", "x"}, "--store is required"},
		{[]string{"build-global", "--pbf", "x", "--store", "y"}, "--work-dir is required"},
		{append(append([]string(nil), base...), "--zoom", "11"), "fixed at 12"},
		{append(append([]string(nil), base...), "--run-memory-mb", "0"), "positive"},
		{append(append([]string(nil), base...), "--pack-mb", "0"), "positive"},
		{append(append([]string(nil), base...), "--open-shards", "0"), "positive"},
		{append(append([]string(nil), base...), "garbage"), "unexpected arguments"},
	} {
		if err := CmdChunks(test.args); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("CmdChunks(%v) error = %v, want %q", test.args, err, test.want)
		}
	}
}

func TestCmdChunksHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := CmdChunksContext(ctx, []string{"build", "--pbf", "unused.osm.pbf", "--store", t.TempDir(), "--region", "test"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CmdChunksContext() error = %v, want context.Canceled", err)
	}
}

func TestCmdChunksReturnsFlagAndSubcommandErrors(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{nil, "build subcommand"},
		{[]string{"unknown"}, "unknown chunks subcommand"},
		{[]string{"build", "--unknown"}, "flag provided"},
		{[]string{"build", "--pbf", "x", "--store", "y"}, "--region is required"},
		{[]string{"build", "--pbf", "x", "--store", "y", "--region", "z", "garbage"}, "unexpected arguments"},
	} {
		if err := CmdChunks(test.args); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("CmdChunks(%v) error = %v, want %q", test.args, err, test.want)
		}
	}
}

func captureCLIStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	runErr := run()
	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data), runErr
}
