package cli

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

func TestCmdServerOpensAndClosesChunkRouter(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "world")
	fixture := filepath.Join("..", "..", "pkg", "plugins", "worldgraph", "builder", "testdata", "seam.osm.pbf")
	if err := CmdChunks([]string{"build", "--pbf", fixture, "--store", storePath, "--region", "server-test"}); err != nil {
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

	original := runHTTPServer
	var capturedHost any
	var capturedAddress string
	var capturedOrigins []string
	runHTTPServer = func(host any, address string, origins ...string) {
		capturedHost, capturedAddress = host, address
		capturedOrigins = append([]string(nil), origins...)
	}
	t.Cleanup(func() { runHTTPServer = original })

	if err := CmdServer([]string{
		"--chunks", storePath, "--chunk-cache-mb", "64", "--route-max-tiles", "32",
		"--route-max-expansions", "2", "--cors-origin", "https://app.example",
	}); err != nil {
		t.Fatal(err)
	}
	router, ok := capturedHost.(*worldgraph.Router)
	if !ok {
		t.Fatalf("server host type = %T", capturedHost)
	}
	if capturedAddress != defaultHTTPAddress || !slices.Equal(capturedOrigins, []string{"https://app.example"}) {
		t.Fatalf("server call = %q %v", capturedAddress, capturedOrigins)
	}
	tile := manifest.Tiles[0]
	if _, err := router.ChunkGeoJSON(context.Background(), tile.Z, tile.X, tile.Y); !errors.Is(err, worldgraph.ErrRouterClosed) {
		t.Fatalf("router after CmdServer() = %v, want ErrRouterClosed", err)
	}
}

func TestCmdServerValidatesGraphHostFlags(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{nil, "exactly one of --file or --chunks"},
		{[]string{"--file", "map.osm", "--chunks", "world"}, "exactly one of --file or --chunks"},
		{[]string{"--chunks", "world", "--chunk-cache-mb", "-1"}, "--chunk-cache-mb"},
		{[]string{"--chunks", "world", "--route-max-tiles", "0"}, "--route-max-tiles"},
		{[]string{"--chunks", "world", "--route-max-expansions", "0"}, "--route-max-expansions"},
		{[]string{"--chunks", "world", "--gtfs", "gtfs"}, "--gtfs is not supported with --chunks"},
	} {
		if err := CmdServer(test.args); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("CmdServer(%v) error = %v, want %q", test.args, err, test.want)
		}
	}
}

func TestDefaultHTTPAddressIsLoopback(t *testing.T) {
	if defaultHTTPAddress != "127.0.0.1:8080" {
		t.Fatalf("default HTTP address = %q", defaultHTTPAddress)
	}
}

func TestParseCORSOrigins(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "empty", value: "", want: nil},
		{name: "one", value: "https://app.example", want: []string{"https://app.example"}},
		{name: "many", value: "https://a.example,https://b.example", want: []string{"https://a.example", "https://b.example"}},
		{name: "spaces", value: " https://a.example, , https://b.example ", want: []string{"https://a.example", "https://b.example"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseCORSOrigins(test.value); !slices.Equal(got, test.want) {
				t.Fatalf("parseCORSOrigins(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}
