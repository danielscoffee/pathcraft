package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func TestServer_ConfigDefaultsWithoutGraph(t *testing.T) {
	e := engine.New()
	s := NewServer(e)

	req, err := http.NewRequest("GET", "/config", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q; want application/json", got)
	}

	var body configResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.CenterLat != -8.0540 || body.CenterLon != -34.8800 {
		t.Fatalf("center = (%v, %v); want Recife defaults", body.CenterLat, body.CenterLon)
	}
	if body.Zoom != 16 {
		t.Fatalf("zoom = %d; want 16", body.Zoom)
	}
	if body.TileURL == "" {
		t.Fatal("tile_url empty")
	}
	if body.GraphChunks != nil {
		t.Fatalf("graph_chunks = %+v, want omitted", body.GraphChunks)
	}
}

func TestServer_ConfigCentersOnGraph(t *testing.T) {
	e := newTestEngine(t)
	s := NewServer(e)

	req, err := http.NewRequest("GET", "/config", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rr.Code)
	}

	var body configResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// example.osm is near Recife; centroid must land in its bounding area.
	if body.CenterLat > -7.9 || body.CenterLat < -8.2 {
		t.Fatalf("center_lat = %v; expected graph centroid near -8.05", body.CenterLat)
	}
	if body.CenterLon > -34.7 || body.CenterLon < -35.0 {
		t.Fatalf("center_lon = %v; expected graph centroid near -34.88", body.CenterLon)
	}
}
