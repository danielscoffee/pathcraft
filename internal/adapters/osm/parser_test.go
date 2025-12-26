package osm_test

import (
	"strings"
	"testing"

	. "github.com/danielscoffee/pathcraft/internal/adapters/osm"
	"github.com/danielscoffee/pathcraft/internal/domain/geo"
)

const testOSMXML = `<?xml version="1.0" encoding="UTF-8"?>
<osm version="0.6">
  <node id="1" lat="55.6761" lon="12.5683"/>
  <node id="2" lat="55.6771" lon="12.5693"/>
  <node id="3" lat="55.6781" lon="12.5703"/>
  <node id="4" lat="55.6791" lon="12.5713">
    <tag k="name" v="Test Point"/>
  </node>
  <way id="100">
    <nd ref="1"/>
    <nd ref="2"/>
    <nd ref="3"/>
    <tag k="highway" v="footway"/>
    <tag k="name" v="Test Path"/>
  </way>
  <way id="101">
    <nd ref="2"/>
    <nd ref="4"/>
    <tag k="highway" v="motorway"/>
  </way>
  <way id="102">
    <nd ref="3"/>
    <nd ref="4"/>
    <tag k="highway" v="residential"/>
    <tag k="foot" v="no"/>
  </way>
</osm>`

func TestParseXML(t *testing.T) {
	data, err := ParseXML(strings.NewReader(testOSMXML))
	if err != nil {
		t.Fatalf("ParseXML() error = %v", err)
	}

	// Check nodes
	if len(data.Nodes) != 4 {
		t.Errorf("expected 4 nodes, got %d", len(data.Nodes))
	}

	node1 := data.Nodes[1]
	if node1 == nil {
		t.Fatal("node 1 not found")
	}
	if node1.Lat != 55.6761 || node1.Lon != 12.5683 {
		t.Errorf("node1 coords = (%v, %v), want (55.6761, 12.5683)", node1.Lat, node1.Lon)
	}

	node4 := data.Nodes[4]
	if node4.Tags["name"] != "Test Point" {
		t.Errorf("node4 name = %q, want %q", node4.Tags["name"], "Test Point")
	}

	// Check ways
	if len(data.Ways) != 3 {
		t.Errorf("expected 3 ways, got %d", len(data.Ways))
	}

	var footway *Way
	for _, w := range data.Ways {
		if w.ID == 100 {
			footway = w
			break
		}
	}

	if footway == nil {
		t.Fatal("way 100 not found")
	}
	if len(footway.NodeIDs) != 3 {
		t.Errorf("footway has %d nodes, want 3", len(footway.NodeIDs))
	}
	if footway.Tags["highway"] != "footway" {
		t.Errorf("footway highway = %q, want %q", footway.Tags["highway"], "footway")
	}
}

func TestFilterIsWalkable(t *testing.T) {
	filter := DefaultFilter()

	tests := []struct {
		name     string
		tags     map[string]string
		expected bool
	}{
		{"footway", map[string]string{"highway": "footway"}, true},
		{"path", map[string]string{"highway": "path"}, true},
		{"residential", map[string]string{"highway": "residential"}, true},
		{"motorway", map[string]string{"highway": "motorway"}, false},
		{"no foot", map[string]string{"highway": "footway", "foot": "no"}, false},
		{"private access", map[string]string{"highway": "path", "access": "private"}, false},
		{"no highway tag", map[string]string{"name": "test"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &Way{Tags: tt.tags}
			got := filter.IsWalkable(w)
			if got != tt.expected {
				t.Errorf("IsWalkable(%v) = %v, want %v", tt.tags, got, tt.expected)
			}
		})
	}
}

func TestFilterWays(t *testing.T) {
	data, err := ParseXML(strings.NewReader(testOSMXML))
	if err != nil {
		t.Fatalf("ParseXML() error = %v", err)
	}

	filter := DefaultFilter()
	walkable := data.FilterWays(filter)

	// Only way 100 (footway) should be walkable
	// way 101 is motorway (not walkable)
	// way 102 is residential but has foot=no
	if len(walkable) != 1 {
		t.Errorf("expected 1 walkable way, got %d", len(walkable))
	}

	if len(walkable) > 0 && walkable[0].ID != 100 {
		t.Errorf("expected way 100, got way %d", walkable[0].ID)
	}
}

func TestBuildGraph(t *testing.T) {
	data, err := ParseXML(strings.NewReader(testOSMXML))
	if err != nil {
		t.Fatalf("ParseXML() error = %v", err)
	}

	g := BuildGraph(data, nil)

	// Should have nodes 1, 2, 3 from the walkable footway
	if !g.HasNode(1) || !g.HasNode(2) || !g.HasNode(3) {
		t.Error("graph should have nodes 1, 2, 3")
	}

	// Node 4 should NOT be in graph (only used by non-walkable ways)
	if g.HasNode(4) {
		t.Error("graph should not have node 4")
	}

	// Check edges exist
	neighbors1 := g.Neighbors(1)
	if len(neighbors1) != 1 {
		t.Errorf("node 1 should have 1 neighbor, got %d", len(neighbors1))
	}

	neighbors2 := g.Neighbors(2)
	if len(neighbors2) != 2 {
		t.Errorf("node 2 should have 2 neighbors (1 and 3), got %d", len(neighbors2))
	}
}

func TestHaversineDistance(t *testing.T) {
	// Copenhagen Central Station to Nørreport Station is ~1.5km
	lat1, lon1 := 55.6726, 12.5648 // Copenhagen Central
	lat2, lon2 := 55.6833, 12.5717 // Nørreport

	dist := geo.HaversineDistance(lat1, lon1, lat2, lon2)

	// Should be approximately 1300-1400m
	if dist < 1200 || dist > 1500 {
		t.Errorf("HaversineDistance = %v, expected ~1300m", dist)
	}
}
