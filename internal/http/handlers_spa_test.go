package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

func TestServer_GraphVisualRedirectsToRoot(t *testing.T) {
	s := NewServer(engine.New())

	req, err := http.NewRequest("GET", "/graph-visual", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d; want 301", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Fatalf("location = %q; want /", loc)
	}
}

func TestServer_RootServesSPAOrBuildHint(t *testing.T) {
	s := NewServer(engine.New())

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	// The embedded bundle depends on whether web/app/dist was built before
	// compiling; both outcomes must be well-formed.
	switch rr.Code {
	case http.StatusOK:
		if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("content type = %q; want text/html", ct)
		}
		if !strings.Contains(rr.Body.String(), "<div id=\"root\">") {
			t.Fatalf("expected SPA root element in body")
		}
	case http.StatusServiceUnavailable:
		if !strings.Contains(rr.Body.String(), "make web") {
			t.Fatalf("expected build hint in 503 body, got %q", rr.Body.String())
		}
	default:
		t.Fatalf("status = %d; want 200 or 503", rr.Code)
	}
}

func TestServer_SPAFallbackForUnknownPath(t *testing.T) {
	s := NewServer(engine.New())

	req, err := http.NewRequest("GET", "/some/client/route", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK && rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 200 (index fallback) or 503 (not built)", rr.Code)
	}
}
