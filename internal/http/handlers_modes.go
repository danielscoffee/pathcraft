package http

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
)

func (s *Server) handleModes(w http.ResponseWriter, _ *http.Request) {
	names := s.registry.Modes()
	manifests := make([]core.ModeManifest, 0, len(names))
	for _, name := range names {
		mode, ok := s.registry.Mode(name)
		if ok {
			manifests = append(manifests, mode.Manifest())
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(modeResponse{Modes: manifests}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleModeRoute(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	name := query.Get("mode")
	if name == "" {
		http.Error(w, "mode parameter required", http.StatusBadRequest)
		return
	}
	mode, ok := s.registry.Mode(name)
	if !ok {
		http.Error(w, fmt.Sprintf("mode %q not registered", name), http.StatusNotFound)
		return
	}
	from, err := parseModePosition("from", query.Get("from"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	to, err := parseModePosition("to", query.Get("to"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	options := make(map[string]string, len(query))
	for key, values := range query {
		if key == "mode" || key == "from" || key == "to" || len(values) == 0 {
			continue
		}
		options[key] = values[0]
	}

	result, err := mode.Route(r.Context(), s.modeHost, core.ModeRequest{
		From:    from,
		To:      to,
		Options: options,
	})
	if err != nil {
		writeModeHostError(w, s.modeHost, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func parseModePosition(name, value string) (core.Position, error) {
	if value == "" {
		return nil, fmt.Errorf("%s parameter required", name)
	}
	parts := strings.Split(value, ",")
	position := make(core.Position, len(parts))
	for i, part := range parts {
		coordinate, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || math.IsNaN(coordinate) || math.IsInf(coordinate, 0) {
			return nil, fmt.Errorf("invalid %s position", name)
		}
		position[i] = coordinate
	}
	return position, nil
}
