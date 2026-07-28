package air

import (
	"context"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

func TestPluginRoutesInThreeDimensions(t *testing.T) {
	result, err := (Plugin{}).Route(context.Background(), nil, core.ModeRequest{
		From: core.Position{12.5648, 55.6726, 100},
		To:   core.Position{12.5717, 55.6833, 200},
		Options: map[string]string{
			"cruise_altitude_m": "1500",
			"speed_mps":         "200",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "air" || len(result.Segments) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	positions := result.Segments[0].Positions
	if len(positions) != 3 {
		t.Fatalf("positions = %v, want start/cruise/end", positions)
	}
	for _, position := range positions {
		if len(position) != 3 {
			t.Fatalf("position = %v, want 3 dimensions", position)
		}
	}
	if positions[1][2] != 1500 {
		t.Fatalf("cruise altitude = %v, want 1500", positions[1][2])
	}
	if result.DistanceMeters <= 0 || result.DurationSeconds <= 0 {
		t.Fatalf("expected positive metrics, got %+v", result)
	}
}

func TestPluginRegistersItself(t *testing.T) {
	mode, ok := plugins.Default.Mode("air")
	if !ok || mode.Manifest().CRS != "EPSG:4326" {
		t.Fatalf("air mode not registered: %#v, %v", mode, ok)
	}
}
