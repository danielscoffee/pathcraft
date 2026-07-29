package cli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
)

func TestCmdRouteUsesChunkHostForStreetMode(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "world")
	fixture := filepath.Join("..", "..", "pkg", "plugins", "worldgraph", "builder", "testdata", "seam.osm.pbf")
	if err := CmdChunks([]string{"build", "--pbf", fixture, "--store", storePath, "--region", "route-test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := captureCLIStdout(t, func() error {
		return CmdRoute([]string{
			"--chunks", storePath, "--mode", "walk",
			"--from-position", "12.5683,55.6761", "--to-position", "12.5685,55.6762",
		})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCmdRouteRequiresOneStreetHostAndLeavesAirHostless(t *testing.T) {
	positions := []string{"--mode", "walk", "--from-position", "0,1", "--to-position", "1,1"}
	if err := CmdRoute(positions); err == nil || !strings.Contains(err.Error(), "exactly one of --file or --chunks") {
		t.Fatalf("hostless walk error = %v", err)
	}
	both := append([]string{"--file", "legacy.osm", "--chunks", "world"}, positions...)
	if err := CmdRoute(both); err == nil || !strings.Contains(err.Error(), "exactly one of --file or --chunks") {
		t.Fatalf("dual host error = %v", err)
	}
	if _, err := captureCLIStdout(t, func() error {
		return CmdRoute([]string{"--mode", "air", "--from-position", "0,1", "--to-position", "1,1"})
	}); err != nil {
		t.Fatalf("hostless air route: %v", err)
	}
	if err := CmdRoute([]string{"--mode", "air", "--file", "missing.osm", "--from-position", "0,1", "--to-position", "1,1"}); err == nil || !strings.Contains(err.Error(), "does not use") {
		t.Fatalf("air host flag error = %v", err)
	}
}

func TestCmdRouteTracksZeroCoordinateFlagPresence(t *testing.T) {
	if _, err := captureCLIStdout(t, func() error {
		return CmdRoute([]string{
			"--mode", "air", "--from-lon", "0", "--from-lat", "1",
			"--to-lon", "1", "--to-lat", "0",
		})
	}); err != nil {
		t.Fatalf("zero coordinate route: %v", err)
	}
	if err := CmdRoute([]string{"--mode", "air", "--from-lon", "0", "--from-lat", "1"}); err == nil || !strings.Contains(err.Error(), "required together") {
		t.Fatalf("partial coordinate error = %v", err)
	}
}

func TestCmdRouteReturnsFlagErrors(t *testing.T) {
	if err := CmdRoute([]string{"--unknown"}); err == nil {
		t.Fatal("CmdRoute() error = nil")
	}
	if err := CmdRoute([]string{"--mode", "air", "--from-position", "0,1", "--to-position", "1,1", "garbage"}); err == nil || !strings.Contains(err.Error(), "unexpected arguments") {
		t.Fatalf("trailing argument error = %v", err)
	}
}

func TestParseCLIPositionPreservesDimensions(t *testing.T) {
	got, err := parseCLIPosition("from", "1.5, 2.5, 300")
	if err != nil {
		t.Fatal(err)
	}
	want := core.Position{1.5, 2.5, 300}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("position = %v, want %v", got, want)
	}
}
