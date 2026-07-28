package gtfsmode

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func TestPluginReturnsGenericJourneySegments(t *testing.T) {
	e := engine.New()
	root := filepath.Join("..", "..", "..", "testdata")
	if err := e.LoadOSM(filepath.Join(root, "example.osm")); err != nil {
		t.Fatal(err)
	}
	if err := e.LoadGTFSDir(filepath.Join(root, "mini_gtfs")); err != nil {
		t.Fatal(err)
	}

	result, err := (Plugin{}).Route(context.Background(), e, core.ModeRequest{
		From:    core.Position{-34.88130, -8.05428},
		To:      core.Position{-34.88030, -8.05480},
		Options: map[string]string{"departure_time": "05:00:00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "gtfs" || len(result.Segments) == 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.DurationSeconds <= 0 {
		t.Fatalf("expected positive duration, got %+v", result)
	}
	for _, segment := range result.Segments {
		for _, position := range segment.Positions {
			if len(position) != 2 {
				t.Fatalf("expected 2D journey position, got %v", position)
			}
		}
	}
}
