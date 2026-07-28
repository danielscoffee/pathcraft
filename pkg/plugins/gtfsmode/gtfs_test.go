package gtfsmode

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

type captureHost struct {
	request engine.MultimodalRouteRequest
}

func (host *captureHost) MultimodalRoute(request engine.MultimodalRouteRequest) (*engine.MultimodalRouteResult, error) {
	host.request = request
	return &engine.MultimodalRouteResult{Mode: "walk", TotalDuration: time.Second}, nil
}

func TestPluginOwnsJourneyDefaults(t *testing.T) {
	host := &captureHost{}
	_, err := (Plugin{}).Route(context.Background(), host, core.ModeRequest{
		From: core.Position{0, 0},
		To:   core.Position{1, 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if host.request.MaxStopCount != 8 {
		t.Fatalf("MaxStopCount = %d, want 8", host.request.MaxStopCount)
	}
	if got := host.request.WalkingProfile.Speed(); got != 1.4 {
		t.Fatalf("walking speed = %v, want 1.4", got)
	}
}

func TestPluginRejectsExcessiveStopCandidates(t *testing.T) {
	host := &captureHost{}
	_, err := (Plugin{}).Route(context.Background(), host, core.ModeRequest{
		From:    core.Position{0, 0},
		To:      core.Position{1, 1},
		Options: map[string]string{"max_stop_count": "65"},
	})
	if err == nil || !strings.Contains(err.Error(), "between 0 and 64") {
		t.Fatalf("Route() error = %v, want bounded stop-candidate rejection", err)
	}
}

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
