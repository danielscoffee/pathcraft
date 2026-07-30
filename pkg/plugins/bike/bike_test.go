package bike

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

type cancelHost struct{ started chan struct{} }

func (host cancelHost) RouteByCoordinatesContext(ctx context.Context, _ engine.CoordinateRouteRequest) (*engine.CoordinateRouteResult, error) {
	close(host.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestPluginPassesCancellationToStreetHost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	host := cancelHost{started: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := (Plugin{}).Route(ctx, host, core.ModeRequest{From: core.Position{0, 1}, To: core.Position{1, 1}})
		done <- err
	}()
	select {
	case <-host.started:
		cancel()
	case err := <-done:
		t.Fatalf("street host was not called: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Route() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Route() did not propagate cancellation")
	}
}

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
