package http

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielscoffee/pathcraft/internal/logging"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/engine"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
	"go.uber.org/zap"
)

func NewServer(e *engine.Engine, allowedOrigins ...string) *Server {
	return NewServerWithRegistry(e, plugins.Default, allowedOrigins...)
}

func NewServerWithRegistry(e *engine.Engine, registry *plugins.Registry, allowedOrigins ...string) *Server {
	if registry == nil {
		registry = plugins.Default
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if origin = strings.TrimSpace(origin); validOrigin(origin) {
			origins[origin] = struct{}{}
		}
	}
	return &Server{engine: e, registry: registry, allowedOrigins: origins}
}

func validOrigin(origin string) bool {
	if origin == "" || origin == "null" || origin == "*" {
		return false
	}
	parsed, err := url.Parse(origin)
	return err == nil &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Host != "" && parsed.User == nil && parsed.Path == "" &&
		parsed.RawQuery == "" && parsed.Fragment == ""
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/journey", s.handleJourney)
	mux.HandleFunc("/modes", s.handleModes)
	mux.HandleFunc("/mode-route", s.handleModeRoute)
	mux.HandleFunc("/route", s.handleRoute)
	mux.HandleFunc("/transit/stops", s.handleTransitStops)
	mux.HandleFunc("/transit/trips", s.handleTransitTrips)
	mux.HandleFunc("/transit/trip", s.handleTransitTrip)
	mux.HandleFunc("/nearest", s.handleNearest)
	mux.HandleFunc("/graph", s.handleGraph)
	mux.HandleFunc("/nodes", s.handleNodes)
	mux.HandleFunc("/config", s.handleConfig)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	// Legacy demo URL; the SPA now lives at the root.
	mux.Handle("/graph-visual", http.RedirectHandler("/", http.StatusMovedPermanently))
	mux.Handle("/", spaHandler())
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mux.ServeHTTP(w, r)
	})
	if len(s.allowedOrigins) == 0 {
		return handler
	}
	return s.crossOrigin(handler)
}

func (s *Server) crossOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		requestedMethod := r.Header.Get("Access-Control-Request-Method")
		preflight := r.Method == http.MethodOptions && requestedMethod != ""
		if preflight {
			w.Header().Add("Vary", "Access-Control-Request-Method")
		}

		if _, allowed := s.allowedOrigins[origin]; !allowed {
			if preflight {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		if !preflight && r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		if !preflight {
			next.ServeHTTP(w, r)
			return
		}
		if requestedMethod != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "CORS method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Access-Control-Allow-Methods", http.MethodGet)
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.handleStatus(w, r)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte(`{"status":"health"}`))
	if err != nil {
		http.Error(w, `{"status": "not health"}`, http.StatusInternalServerError)
		return
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func RunServer(e *engine.Engine, addr string, allowedOrigins ...string) {
	s := NewServer(e, allowedOrigins...)
	logging.L().Info("server starting", zap.String("addr", addr))
	if err := newHTTPServer(addr, s.Handler()).ListenAndServe(); err != nil {
		logging.L().Fatal("server stopped", zap.String("addr", addr), zap.Error(err))
	}
}
