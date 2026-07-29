package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

type chunkHostStub struct {
	generation string
	data       []byte
	err        error
	routeCalls int
}

func (host *chunkHostStub) ChunkConfig() (string, int, int) {
	return host.generation, 12, 11
}

func (host *chunkHostStub) ChunkGeoJSON(context.Context, int, int, int) ([]byte, error) {
	return host.data, host.err
}

func (host *chunkHostStub) NearestPosition(context.Context, float64, float64) (int64, float64, float64, float64, error) {
	return 42, 1.25, 2.5, 3.75, host.err
}

func (host *chunkHostStub) RouteByCoordinatesContext(context.Context, engine.CoordinateRouteRequest) (*engine.CoordinateRouteResult, error) {
	host.routeCalls++
	if host.err != nil {
		return nil, host.err
	}
	return &engine.CoordinateRouteResult{
		RouteResult: engine.RouteResult{
			Nodes: []int64{1, 2}, Coordinates: []engine.Coordinate{{Lat: 1, Lon: 2}, {Lat: 3, Lon: 4}},
			Distance: 100,
		},
		FromNodeID: 1, ToNodeID: 2,
	}, nil
}

func TestServerConfigAdvertisesGraphChunksOnlyForCapableHost(t *testing.T) {
	host := &chunkHostStub{generation: "generation-a"}
	rr := httptest.NewRecorder()
	NewServerWithHost(host).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"graph_chunks":{"generation":"generation-a","zoom":12,"min_render_zoom":11,"url":"/graph/chunks/{generation}/{z}/{x}/{y}"}`) {
		t.Fatalf("config = %s", rr.Body.String())
	}

	legacy := httptest.NewRecorder()
	NewServer(newTestEngine(t)).Handler().ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/config", nil))
	if strings.Contains(legacy.Body.String(), "graph_chunks") {
		t.Fatalf("legacy config unexpectedly advertises chunks: %s", legacy.Body.String())
	}
}

func TestServerServesImmutableGraphChunk(t *testing.T) {
	host := &chunkHostStub{generation: "generation-a", data: []byte(`{"type":"FeatureCollection","features":[]}`)}
	handler := NewServerWithHost(host).Handler()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/graph/chunks/generation-a/12/1/2", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "application/geo+json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rr.Header().Get("ETag"); got != `"generation-a"` {
		t.Fatalf("ETag = %q", got)
	}
	if got := rr.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("Cache-Control = %q", got)
	}
	if rr.Body.String() != string(host.data) {
		t.Fatalf("body = %s", rr.Body.String())
	}

	for _, validator := range []string{`"generation-a"`, `W/"generation-a"`} {
		conditional := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/graph/chunks/generation-a/12/1/2", nil)
		request.Header.Set("If-None-Match", validator)
		handler.ServeHTTP(conditional, request)
		if conditional.Code != http.StatusNotModified || conditional.Body.Len() != 0 {
			t.Fatalf("conditional response for %s = %d %q", validator, conditional.Code, conditional.Body.String())
		}
	}
}

func TestServerMapsGraphChunkErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		path       string
		err        error
		wantStatus int
	}{
		{name: "invalid tile", path: "/graph/chunks/generation-a/nope/1/2", wantStatus: http.StatusBadRequest},
		{name: "invalid tile precedes stale generation", path: "/graph/chunks/old/nope/1/2", wantStatus: http.StatusBadRequest},
		{name: "out of range tile", path: "/graph/chunks/generation-a/12/4096/0", wantStatus: http.StatusBadRequest},
		{name: "stale generation", path: "/graph/chunks/old/12/1/2", wantStatus: http.StatusConflict},
		{name: "uncovered", path: "/graph/chunks/generation-a/12/1/2", err: worldgraph.ErrUncoveredTile, wantStatus: http.StatusNotFound},
		{name: "missing", path: "/graph/chunks/generation-a/12/1/2", err: worldgraph.ErrMissingChunk, wantStatus: http.StatusServiceUnavailable},
		{name: "corrupt", path: "/graph/chunks/generation-a/12/1/2", err: worldgraph.ErrCorruptChunk, wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			host := &chunkHostStub{generation: "generation-a", err: test.err}
			rr := httptest.NewRecorder()
			NewServerWithHost(host).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, test.path, nil))
			if rr.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rr.Code, test.wantStatus, rr.Body.String())
			}
		})
	}

	invalidWithoutHost := httptest.NewRecorder()
	NewServerWithHost(struct{}{}).Handler().ServeHTTP(invalidWithoutHost, httptest.NewRequest(http.MethodGet, "/graph/chunks/generation-a/nope/1/2", nil))
	if invalidWithoutHost.Code != http.StatusBadRequest {
		t.Fatalf("invalid tile without host status = %d, want 400", invalidWithoutHost.Code)
	}

	host := &chunkHostStub{generation: "generation-a", err: errors.New("open /secret/store: permission denied")}
	rr := httptest.NewRecorder()
	NewServerWithHost(host).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/graph/chunks/generation-a/12/1/2", nil))
	if rr.Code != http.StatusInternalServerError || strings.Contains(rr.Body.String(), "/secret/store") {
		t.Fatalf("unknown backend error = %d %q", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") != "" || rr.Header().Get("ETag") != "" {
		t.Fatalf("error cache headers = %v", rr.Header())
	}
}

func TestChunkHostServerKeepsLegacyEndpointsUnavailable(t *testing.T) {
	handler := NewServerWithHost(&chunkHostStub{generation: "generation-a"}).Handler()
	for _, path := range []string{
		"/graph", "/nodes", "/transit/stops", "/transit/trips", "/transit/trip?trip_id=x",
		"/journey?from_lat=1&from_lon=2&to_lat=3&to_lon=4&time=05:00",
	} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want 503; body=%s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestServerUsesChunkHostForNearestAndStreetModes(t *testing.T) {
	host := &chunkHostStub{generation: "generation-a"}
	handler := NewServerWithHost(host).Handler()
	nearest := httptest.NewRecorder()
	handler.ServeHTTP(nearest, httptest.NewRequest(http.MethodGet, "/nearest?lat=1&lon=2", nil))
	if nearest.Code != http.StatusOK || !strings.Contains(nearest.Body.String(), `"id": 42`) || !strings.Contains(nearest.Body.String(), `"lat": 1.250000`) {
		t.Fatalf("nearest = %d %s", nearest.Code, nearest.Body.String())
	}

	route := httptest.NewRecorder()
	handler.ServeHTTP(route, httptest.NewRequest(http.MethodGet, "/route?from_lat=1&from_lon=2&to_lat=3&to_lon=4&mode=walk", nil))
	if route.Code != http.StatusOK || host.routeCalls != 1 {
		t.Fatalf("route = %d %s, calls=%d", route.Code, route.Body.String(), host.routeCalls)
	}
	modeRoute := httptest.NewRecorder()
	handler.ServeHTTP(modeRoute, httptest.NewRequest(http.MethodGet, "/mode-route?mode=walk&from=2,1&to=4,3", nil))
	if modeRoute.Code != http.StatusOK || host.routeCalls != 2 {
		t.Fatalf("mode route = %d %s, calls=%d", modeRoute.Code, modeRoute.Body.String(), host.routeCalls)
	}
	for _, path := range []string{
		"/mode-route?mode=walk&from=2,1&to=4,3&speed_mps=bogus",
		"/mode-route?mode=walk&from=2,91&to=4,3",
	} {
		invalid := httptest.NewRecorder()
		handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, path, nil))
		if invalid.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid mode route %q status = %d, want 422; body=%s", path, invalid.Code, invalid.Body.String())
		}
	}

	host.err = worldgraph.ErrUncoveredTile
	uncovered := httptest.NewRecorder()
	handler.ServeHTTP(uncovered, httptest.NewRequest(http.MethodGet, "/nearest?lat=1&lon=2", nil))
	if uncovered.Code != http.StatusNotFound || !errors.Is(host.err, worldgraph.ErrUncoveredTile) {
		t.Fatalf("nearest uncovered = %d %s", uncovered.Code, uncovered.Body.String())
	}

	for _, test := range []struct {
		err        error
		wantStatus int
	}{
		{worldgraph.ErrMissingChunk, http.StatusServiceUnavailable},
		{worldgraph.ErrCorruptChunk, http.StatusServiceUnavailable},
		{worldgraph.ErrRouteAreaLimit, http.StatusRequestEntityTooLarge},
		{worldgraph.ErrNoPath, http.StatusNotFound},
		{context.Canceled, http.StatusRequestTimeout},
	} {
		host.err = test.err
		route := httptest.NewRecorder()
		handler.ServeHTTP(route, httptest.NewRequest(http.MethodGet, "/route?from_lat=1&from_lon=2&to_lat=3&to_lon=4&mode=walk", nil))
		if route.Code != test.wantStatus {
			t.Fatalf("route error %v status = %d, want %d", test.err, route.Code, test.wantStatus)
		}
		modeRoute := httptest.NewRecorder()
		handler.ServeHTTP(modeRoute, httptest.NewRequest(http.MethodGet, "/mode-route?mode=walk&from=2,1&to=4,3", nil))
		if modeRoute.Code != test.wantStatus {
			t.Fatalf("mode route error %v status = %d, want %d", test.err, modeRoute.Code, test.wantStatus)
		}
	}
}
