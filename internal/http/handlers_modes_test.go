package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

type fakeMode struct{}

func (fakeMode) Name() string { return "space" }
func (fakeMode) Manifest() core.ModeManifest {
	return core.ModeManifest{
		ID:         "space",
		Label:      "Space",
		Dimensions: []int{3},
		Axes:       []string{"x", "y", "z"},
	}
}
func (fakeMode) Route(_ context.Context, _ any, req core.ModeRequest) (core.ModeResult, error) {
	return core.ModeResult{
		Mode: "space",
		Segments: []core.RouteSegment{{
			Kind:      "space",
			Positions: []core.Position{req.From, req.To},
		}},
		Meta: map[string]any{"thrust": req.Options["thrust"]},
	}, nil
}

func TestModesComeFromRegistry(t *testing.T) {
	registry := plugins.New()
	if err := registry.RegisterMode(fakeMode{}); err != nil {
		t.Fatal(err)
	}
	handler := NewServerWithRegistry(engine.New(), registry).Handler()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/modes", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var response modeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Modes) != 1 || response.Modes[0].ID != "space" {
		t.Fatalf("modes = %+v", response.Modes)
	}
}

func TestModeRoutePreservesDimensionsAndOptions(t *testing.T) {
	registry := plugins.New()
	if err := registry.RegisterMode(fakeMode{}); err != nil {
		t.Fatal(err)
	}
	handler := NewServerWithRegistry(engine.New(), registry).Handler()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet,
		"/mode-route?mode=space&from=1,2,3&to=4,5,6&thrust=high", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var result core.ModeResult
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Segments) != 1 || len(result.Segments[0].Positions[0]) != 3 {
		t.Fatalf("result = %+v", result)
	}
	if result.Meta["thrust"] != "high" {
		t.Fatalf("options not forwarded: %+v", result.Meta)
	}
}

func TestModeRouteRunsRegisteredAirPlugin(t *testing.T) {
	handler := NewServer(engine.New()).Handler()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet,
		"/mode-route?mode=air&from=12.5648,55.6726,100&to=12.5717,55.6833,200&cruise_altitude_m=1500", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var result core.ModeResult
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	positions := result.Segments[0].Positions
	if len(positions) != 3 || positions[1][2] != 1500 {
		t.Fatalf("air positions = %v", positions)
	}
}
