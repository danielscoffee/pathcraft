package wasmapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type testEnvelope struct {
	OK    bool            `json:"ok"`
	Value json.RawMessage `json:"value"`
	Error string          `json:"error"`
}

func decodeEnvelope(t *testing.T, raw string) testEnvelope {
	t.Helper()
	var envelope testEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, raw)
	}
	return envelope
}

func TestRouteRequiresLoadedGraph(t *testing.T) {
	response := decodeEnvelope(t, New().Route(`{
		"from":{"lat":-8.05428,"lon":-34.88130},
		"to":{"lat":-8.05520,"lon":-34.87970}
	}`))
	if response.OK || response.Error != "graph not loaded" {
		t.Fatalf("Route() = %+v, want graph-not-loaded error", response)
	}
}

func TestLoadOSMAndRoute(t *testing.T) {
	xml, err := os.ReadFile("../../testdata/example.osm")
	if err != nil {
		t.Fatal(err)
	}
	api := New()
	if response := decodeEnvelope(t, api.LoadOSM(string(xml))); !response.OK {
		t.Fatalf("LoadOSM() error = %q", response.Error)
	}

	statsResponse := decodeEnvelope(t, api.Stats())
	var stats struct {
		Nodes           int `json:"nodes"`
		ContractedNodes int `json:"contractedNodes"`
	}
	if err := json.Unmarshal(statsResponse.Value, &stats); err != nil {
		t.Fatal(err)
	}
	if !statsResponse.OK || stats.Nodes != 6 || stats.ContractedNodes != 4 {
		t.Fatalf("Stats() = %+v, envelope %+v", stats, statsResponse)
	}

	routeResponse := decodeEnvelope(t, api.Route(`{
		"from":{"lat":-8.05428,"lon":-34.88130},
		"to":{"lat":-8.05520,"lon":-34.87970},
		"mode":"walk",
		"includeCoordinates":true
	}`))
	if !routeResponse.OK {
		t.Fatalf("Route() error = %q", routeResponse.Error)
	}
	var route struct {
		Nodes             []int64 `json:"nodes"`
		Coordinates       []Point `json:"coordinates"`
		DistanceMeters    float64 `json:"distanceMeters"`
		DurationSeconds   float64 `json:"durationSeconds"`
		FromNodeID        int64   `json:"fromNodeId"`
		ToNodeID          int64   `json:"toNodeId"`
		FromSnapDistanceM float64 `json:"fromSnapDistanceMeters"`
		DestinationSnapM  float64 `json:"toSnapDistanceMeters"`
	}
	if err := json.Unmarshal(routeResponse.Value, &route); err != nil {
		t.Fatal(err)
	}
	if len(route.Nodes) != 6 || len(route.Coordinates) != 6 {
		t.Fatalf("route nodes/coordinates = %d/%d, want 6/6", len(route.Nodes), len(route.Coordinates))
	}
	if route.FromNodeID != 1 || route.ToNodeID != 6 || route.DistanceMeters <= 0 || route.DurationSeconds <= 0 {
		t.Fatalf("route = %+v", route)
	}
}

func TestLoadOSMRejectsMalformedXML(t *testing.T) {
	response := decodeEnvelope(t, New().LoadOSM("<osm>"))
	if response.OK || !strings.Contains(response.Error, "parsing OSM XML") {
		t.Fatalf("LoadOSM() = %+v, want parse error", response)
	}
}

func TestRouteRejectsInvalidRequest(t *testing.T) {
	tests := map[string]string{
		"malformed JSON":   `{`,
		"unknown field":    `{"from":{"lat":0,"lon":0},"to":{"lat":0,"lon":0},"extra":true}`,
		"unsupported mode": `{"from":{"lat":0,"lon":0},"to":{"lat":0,"lon":0},"mode":"flight"}`,
		"missing origin":   `{"to":{"lat":0,"lon":0}}`,
		"latitude range":   `{"from":{"lat":91,"lon":0},"to":{"lat":0,"lon":0}}`,
		"longitude range":  `{"from":{"lat":0,"lon":0},"to":{"lat":0,"lon":181}}`,
	}

	api := New()
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			response := decodeEnvelope(t, api.Route(request))
			if response.OK || response.Error == "" {
				t.Fatalf("Route(%s) = %+v, want validation error", request, response)
			}
		})
	}
}
