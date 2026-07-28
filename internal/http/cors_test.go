package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

const testOrigin = "https://app.example"

func corsRequest(t *testing.T, server *Server, method, origin, requestedMethod string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/health", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if requestedMethod != "" {
		req.Header.Set("Access-Control-Request-Method", requestedMethod)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)
	return recorder
}

func TestCORSDisabledByDefault(t *testing.T) {
	recorder := corsRequest(t, NewServer(engine.New()), http.MethodGet, testOrigin, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

func TestCORSVariesWithoutOrigin(t *testing.T) {
	server := NewServer(engine.New(), testOrigin)
	recorder := corsRequest(t, server, http.MethodGet, "", "")
	if got := recorder.Header().Values("Vary"); !strings.Contains(strings.Join(got, ","), "Origin") {
		t.Fatalf("Vary = %q, want Origin", got)
	}
}

func TestCORSAllowsConfiguredOrigin(t *testing.T) {
	server := NewServer(engine.New(), testOrigin)
	recorder := corsRequest(t, server, http.MethodGet, testOrigin, "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != testOrigin {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, testOrigin)
	}
	if got := recorder.Header().Values("Vary"); !strings.Contains(strings.Join(got, ","), "Origin") {
		t.Fatalf("Vary = %q, want Origin", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want empty", got)
	}
}

func TestCORSDoesNotExposeDisallowedSimpleRequest(t *testing.T) {
	server := NewServer(engine.New(), testOrigin)
	recorder := corsRequest(t, server, http.MethodGet, "https://evil.example", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want underlying handler status 200", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

func TestCORSRejectsInvalidOriginConfiguration(t *testing.T) {
	for _, origin := range []string{"*", "null", "file://local", "https://app.example/path"} {
		t.Run(origin, func(t *testing.T) {
			server := NewServer(engine.New(), origin)
			recorder := corsRequest(t, server, http.MethodGet, origin, "")
			if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
			}
		})
	}
}

func TestCORSRejectsActualNonGetMethods(t *testing.T) {
	server := NewServer(engine.New(), testOrigin)
	for _, origin := range []string{"", testOrigin, "https://evil.example"} {
		t.Run(origin, func(t *testing.T) {
			recorder := corsRequest(t, server, http.MethodPost, origin, "")
			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405", recorder.Code)
			}
			if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
			}
		})
	}
}

func TestCORSPreflight(t *testing.T) {
	server := NewServer(engine.New(), testOrigin)
	tests := []struct {
		name            string
		origin          string
		requestedMethod string
		wantStatus      int
		wantOrigin      string
	}{
		{name: "allowed GET", origin: testOrigin, requestedMethod: http.MethodGet, wantStatus: http.StatusNoContent, wantOrigin: testOrigin},
		{name: "disallowed origin", origin: "https://evil.example", requestedMethod: http.MethodGet, wantStatus: http.StatusForbidden},
		{name: "unsupported method", origin: testOrigin, requestedMethod: http.MethodPost, wantStatus: http.StatusMethodNotAllowed, wantOrigin: testOrigin},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := corsRequest(t, server, http.MethodOptions, test.origin, test.requestedMethod)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != test.wantOrigin {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, test.wantOrigin)
			}
			if test.wantStatus == http.StatusNoContent {
				if got := recorder.Header().Get("Access-Control-Allow-Methods"); got != http.MethodGet {
					t.Fatalf("Access-Control-Allow-Methods = %q, want GET", got)
				}
				if recorder.Body.Len() != 0 {
					t.Fatalf("body = %q, want empty", recorder.Body.String())
				}
			}
		})
	}
}
