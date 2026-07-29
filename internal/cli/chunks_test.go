package cli

import (
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
