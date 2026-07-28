package walk

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func TestPluginRoutesStreetCoordinates(t *testing.T) {
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
	if result.Mode != "walk" || len(result.Segments) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Segments[0].Positions) < 2 || len(result.Segments[0].Positions[0]) != 2 {
		t.Fatalf("expected 2D path positions, got %v", result.Segments[0].Positions)
	}
	if result.DurationSeconds <= 0 || result.DistanceMeters <= 0 {
		t.Fatalf("expected positive route metrics, got %+v", result)
	}

	faster, err := (Plugin{}).Route(context.Background(), e, core.ModeRequest{
		From:    core.Position{-34.88130, -8.05428},
		To:      core.Position{-34.88030, -8.05480},
		Options: map[string]string{"speed_mps": "2.8"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if faster.DurationSeconds >= result.DurationSeconds {
		t.Fatalf("speed override duration = %d, default = %d", faster.DurationSeconds, result.DurationSeconds)
	}
}
