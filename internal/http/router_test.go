package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
)

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
