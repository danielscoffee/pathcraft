package http

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func newTestEngine() *engine.Engine {
	e := engine.New()
	if err := e.LoadOSM(filepath.Join("..", "..", "examples", "example.osm")); err != nil {
		panic(err)
	}
	if err := e.LoadGTFSDir(filepath.Join("..", "..", "examples", "mini_gtfs")); err != nil {
		panic(err)
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

func TestServer_RouteByCoordinates(t *testing.T) {
	e := newTestEngine()
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
	e := newTestEngine()
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
	if body := rr.Body.String(); body == "" || body[0] != '{' {
		t.Fatalf("expected json body, got %q", body)
	}
}

func TestServer_TransitStops(t *testing.T) {
	e := newTestEngine()
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
	e := newTestEngine()
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
	e := newTestEngine()
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
