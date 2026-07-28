package bike

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func TestPluginRoutesWithBikePolicy(t *testing.T) {
	e := engine.New()
	if err := e.LoadOSM(filepath.Join("..", "..", "..", "testdata", "example.osm")); err != nil {
		t.Fatal(err)
	}

	result, err := (Plugin{}).Route(context.Background(), e, core.ModeRequest{
		From: core.Position{-34.88130, -8.05428},
		To:   core.Position{-34.88030, -8.05480},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "bike" || len(result.Segments) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.DurationSeconds <= 0 || result.DistanceMeters <= 0 {
		t.Fatalf("expected positive route metrics, got %+v", result)
	}
}
