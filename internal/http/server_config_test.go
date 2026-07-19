package http

import (
	"net/http"
	"testing"
)

func TestNewHTTPServerSetsConnectionTimeouts(t *testing.T) {
	srv := newHTTPServer(":0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout <= 0 {
		t.Fatal("ReadHeaderTimeout must be positive")
	}
	if srv.IdleTimeout <= 0 {
		t.Fatal("IdleTimeout must be positive")
	}
}
