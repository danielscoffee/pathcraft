package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

type graphChunkHost interface {
	ChunkGeoJSON(context.Context, int, int, int) ([]byte, error)
	ChunkConfig() (generation string, zoom, minRenderZoom int)
}

type nearestPositionHost interface {
	NearestPosition(context.Context, float64, float64) (id int64, snapLat, snapLon, distance float64, err error)
}

type graphChunkConfig struct {
	Generation    string `json:"generation"`
	Zoom          int    `json:"zoom"`
	MinRenderZoom int    `json:"min_render_zoom"`
	URL           string `json:"url"`
}

func (s *Server) handleGraphChunk(w http.ResponseWriter, r *http.Request) {
	z, err := strconv.Atoi(r.PathValue("z"))
	if err != nil {
		http.Error(w, "invalid graph chunk tile", http.StatusBadRequest)
		return
	}
	x, err := strconv.Atoi(r.PathValue("x"))
	if err != nil {
		http.Error(w, "invalid graph chunk tile", http.StatusBadRequest)
		return
	}
	y, err := strconv.Atoi(r.PathValue("y"))
	if err != nil || z < 0 || z > 30 {
		http.Error(w, "invalid graph chunk tile", http.StatusBadRequest)
		return
	}
	tileCount := 1 << uint(z)
	if x < 0 || x >= tileCount || y < 0 || y >= tileCount {
		http.Error(w, "invalid graph chunk tile", http.StatusBadRequest)
		return
	}
	host, ok := s.modeHost.(graphChunkHost)
	if !ok {
		http.NotFound(w, r)
		return
	}
	generation, expectedZoom, _ := host.ChunkConfig()
	if z != expectedZoom {
		http.Error(w, "invalid graph chunk tile", http.StatusBadRequest)
		return
	}
	if r.PathValue("generation") != generation {
		http.Error(w, "graph chunk generation is stale", http.StatusConflict)
		return
	}
	data, err := host.ChunkGeoJSON(r.Context(), z, x, y)
	if err != nil {
		status := statusForWorldGraphError(err)
		message := "graph chunk unavailable"
		if status == http.StatusNotFound {
			message = "graph chunk is not covered"
		} else if status == http.StatusRequestTimeout {
			message = "graph chunk request canceled"
		}
		http.Error(w, message, status)
		return
	}
	etag := strconv.Quote(generation)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/geo+json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func etagMatches(header, etag string) bool {
	for value := range strings.SplitSeq(header, ",") {
		value = strings.TrimSpace(value)
		value = strings.TrimPrefix(value, "W/")
		if value == etag || value == "*" {
			return true
		}
	}
	return false
}

func writeModeHostError(w http.ResponseWriter, host any, err error, fallback int) {
	if _, ok := host.(graphChunkHost); !ok {
		http.Error(w, err.Error(), fallback)
		return
	}
	status, recognized := worldGraphErrorStatus(err)
	if !recognized {
		status = fallback
	}
	http.Error(w, http.StatusText(status), status)
}

func statusForWorldGraphError(err error) int {
	status, recognized := worldGraphErrorStatus(err)
	if !recognized {
		return http.StatusInternalServerError
	}
	return status
}

func worldGraphErrorStatus(err error) (int, bool) {
	switch {
	case errors.Is(err, worldgraph.ErrUncoveredTile), errors.Is(err, worldgraph.ErrNoPath):
		return http.StatusNotFound, true
	case errors.Is(err, worldgraph.ErrRouteAreaLimit):
		return http.StatusRequestEntityTooLarge, true
	case errors.Is(err, worldgraph.ErrMissingChunk),
		errors.Is(err, worldgraph.ErrCorruptChunk),
		errors.Is(err, worldgraph.ErrChecksumMismatch),
		errors.Is(err, worldgraph.ErrUnsupportedVersion),
		errors.Is(err, worldgraph.ErrInvalidChunk),
		errors.Is(err, worldgraph.ErrRouterClosed):
		return http.StatusServiceUnavailable, true
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusRequestTimeout, true
	default:
		return 0, false
	}
}
