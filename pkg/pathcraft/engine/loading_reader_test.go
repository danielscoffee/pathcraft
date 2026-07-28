package engine

import (
	"bytes"
	"crypto/sha256"
	"os"
	"slices"
	"testing"
)

func TestLoadOSMReaderPublishesRoutableGraph(t *testing.T) {
	xml, err := os.ReadFile("../../../testdata/example.osm")
	if err != nil {
		t.Fatal(err)
	}

	e := New()
	if err := e.LoadOSMReader(bytes.NewReader(xml)); err != nil {
		t.Fatalf("LoadOSMReader() error = %v", err)
	}

	if e.graphSourceSHA256 != sha256.Sum256(xml) {
		t.Fatalf("engine fingerprint = %x, want SHA-256 of input", e.graphSourceSHA256)
	}
	if stats := e.Stats(); stats.Nodes != 6 || stats.ContractedNodes != 4 {
		t.Fatalf("Stats() = %+v, want 6 nodes and 4 contracted nodes", stats)
	}

	route, err := e.Route(RouteRequest{From: 1, To: 6})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if !slices.Equal(route.Nodes, []int64{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("route nodes = %v, want full fixture path", route.Nodes)
	}
}

func TestLoadOSMReaderRejectsMalformedXML(t *testing.T) {
	e := New()
	if err := e.LoadOSMReader(bytes.NewBufferString("<osm>")); err == nil {
		t.Fatal("LoadOSMReader() error = nil, want malformed XML error")
	}
	if stats := e.Stats(); stats != (GraphStats{}) {
		t.Fatalf("Stats() = %+v after failed load, want empty engine", stats)
	}
}

func TestLoadOSMReaderRejectsNilReader(t *testing.T) {
	if err := New().LoadOSMReader(nil); err == nil {
		t.Fatal("LoadOSMReader(nil) error = nil")
	}
}
