package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func newTestEngine(t *testing.T) *engine.Engine {
	t.Helper()
	e := engine.New()
	if err := e.LoadOSM(filepath.Join("..", "..", "testdata", "example.osm")); err != nil {
		t.Fatalf("LoadOSM: %v", err)
	}
	if err := e.LoadGTFSDir(filepath.Join("..", "..", "testdata", "mini_gtfs")); err != nil {
		t.Fatalf("LoadGTFSDir: %v", err)
	}
	return e
}

// TODO: ADD MORE TESTS
func TestServer_Status(t *testing.T) {
	e := engine.New()
	s := NewServer(e)
	handler := s.Handler()

	req, err := http.NewRequest("GET", "/status", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	expected := `{"status":"health"}`
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), expected)
	}
}

func TestServer_Health(t *testing.T) {
	e := engine.New()
	s := NewServer(e)
	handler := s.Handler()

	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	expected := `{"status":"health"}`
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), expected)
	}
}

func TestServer_Modes(t *testing.T) {
	e := engine.New()
	s := NewServer(e)
	handler := s.Handler()

	req, err := http.NewRequest("GET", "/modes", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handler returned wrong status code: got %v want %v", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}

	var body struct {
		Modes []struct {
			ID       string `json:"id"`
			Label    string `json:"label"`
			Kind     string `json:"kind"`
			Endpoint string `json:"endpoint"`
		} `json:"modes"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode modes response: %v", err)
	}

	expected := []struct {
		id       string
		label    string
		kind     string
		endpoint string
	}{
		{id: "walk", label: "Walk", kind: "standard", endpoint: "/route"},
		{id: "bus", label: "Bus / GTFS", kind: "gtfs", endpoint: "/journey"},
		{id: "car", label: "Car", kind: "standard", endpoint: "/route"},
		{id: "bike", label: "Bike", kind: "standard", endpoint: "/route"},
	}
	if len(body.Modes) != len(expected) {
		t.Fatalf("expected %d modes, got %d", len(expected), len(body.Modes))
	}
	for i, want := range expected {
		got := body.Modes[i]
		if got.ID != want.id || got.Label != want.label || got.Kind != want.kind || got.Endpoint != want.endpoint {
			t.Fatalf("mode %d mismatch: got %+v want %+v", i, got, want)
		}
	}
}

func TestServer_NearestIncludesSnappedCoordinates(t *testing.T) {
	e := newTestEngine(t)
	s := NewServer(e)
	handler := s.Handler()

	req, err := http.NewRequest("GET", "/nearest?lat=-8.05428&lon=-34.88130", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handler returned wrong status code: got %v want %v body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"lat"`) || !strings.Contains(body, `"lon"`) {
		t.Fatalf("expected snapped lat/lon in nearest response, got %q", body)
	}
}

func TestServer_RouteModeControlsOnewayDirection(t *testing.T) {
	e := newTestEngine(t)
	s := NewServer(e)
	handler := s.Handler()

	walkReq, err := http.NewRequest("GET", "/route?from=6&to=5&mode=walk", nil)
	if err != nil {
		t.Fatal(err)
	}
	walkRR := httptest.NewRecorder()
	handler.ServeHTTP(walkRR, walkReq)
	if walkRR.Code != http.StatusOK {
		t.Fatalf("walk reverse oneway status = %d body=%s; want 200", walkRR.Code, walkRR.Body.String())
	}

	carReq, err := http.NewRequest("GET", "/route?from=6&to=5&mode=car", nil)
	if err != nil {
		t.Fatal(err)
	}
	carRR := httptest.NewRecorder()
	handler.ServeHTTP(carRR, carReq)
	if carRR.Code != http.StatusNotFound {
		t.Fatalf("car reverse oneway status = %d body=%s; want 404", carRR.Code, carRR.Body.String())
	}
}

func TestServer_RouteByCoordinates(t *testing.T) {
	e := newTestEngine(t)
	s := NewServer(e)
	handler := s.Handler()

	req, err := http.NewRequest("GET", "/route?from_lat=-8.05428&from_lon=-34.88130&to_lat=-8.05480&to_lon=-34.88030", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handler returned wrong status code: got %v want %v", rr.Code, http.StatusOK)
	}
	if body := rr.Body.String(); body == "" || body[0] != '{' {
		t.Fatalf("expected geojson body, got %q", body)
	}
}

func TestServer_Journey(t *testing.T) {
	e := newTestEngine(t)
	s := NewServer(e)
	handler := s.Handler()

	req, err := http.NewRequest("GET", "/journey?from_lat=-8.05428&from_lon=-34.88130&to_lat=-8.05480&to_lon=-34.88030&time=05:00:00", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handler returned wrong status code: got %v want %v body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var body journeyResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode journey: %v", err)
	}
	if len(body.Legs) == 0 {
		t.Fatal("journey has no legs")
	}
	for _, leg := range body.Legs {
		if leg.DepartureTime == "" || leg.ArrivalTime == "" {
			t.Fatalf("leg missing timing: %+v", leg)
		}
		if leg.DurationSeconds <= 0 {
			t.Fatalf("leg duration = %d, want positive: %+v", leg.DurationSeconds, leg)
		}
	}
}

func TestServer_JourneyAcceptsBrowserTimeWithoutSeconds(t *testing.T) {
	e := newTestEngine(t)
	s := NewServer(e)
	handler := s.Handler()

	req, err := http.NewRequest("GET", "/journey?from_lat=-8.05428&from_lon=-34.88130&to_lat=-8.05480&to_lon=-34.88030&time=05:00", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handler returned wrong status code: got %v want %v body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"departure_time":"05:00:00"`) {
		t.Fatalf("expected normalized departure time in body, got %q", rr.Body.String())
	}
}

func TestServer_TransitStops(t *testing.T) {
	e := newTestEngine(t)
	s := NewServer(e)
	handler := s.Handler()

	req, err := http.NewRequest("GET", "/transit/stops", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handler returned wrong status code: got %v want %v body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "START_STOP") {
		t.Fatalf("expected START_STOP in body, got %q", rr.Body.String())
	}
}

func TestServer_TransitTrips(t *testing.T) {
	e := newTestEngine(t)
	s := NewServer(e)
	handler := s.Handler()

	req, err := http.NewRequest("GET", "/transit/trips", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handler returned wrong status code: got %v want %v body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "SHUTTLE_1") {
		t.Fatalf("expected SHUTTLE_1 in body, got %q", rr.Body.String())
	}
}

func TestServer_TransitTrip(t *testing.T) {
	e := newTestEngine(t)
	s := NewServer(e)
	handler := s.Handler()

	req, err := http.NewRequest("GET", "/transit/trip?trip_id=SHUTTLE_1", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handler returned wrong status code: got %v want %v body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "MID_STOP") || !strings.Contains(body, "LineString") {
		t.Fatalf("expected trip stops and line geometry, got %q", body)
	}
}
