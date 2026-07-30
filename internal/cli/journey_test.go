package cli

import (
	"path/filepath"
	"testing"
)

func TestCmdJourneyDispatchesGTFSMode(t *testing.T) {
	root := filepath.Join("..", "..", "testdata")
	err := CmdJourney([]string{
		"--file", filepath.Join(root, "example.osm"),
		"--gtfs", filepath.Join(root, "mini_gtfs"),
		"--from-lat", "-8.05428",
		"--from-lon", "-34.88130",
		"--to-lat", "-8.05480",
		"--to-lon", "-34.88030",
		"--time", "05:00:00",
	})
	if err != nil {
		t.Fatal(err)
	}
}
