package osm_test

import (
	"strings"
	"testing"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/osm"
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
	data, err := osm.ParseXML(strings.NewReader(testOSMXML))
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

	var footway *osm.Way
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
	filter := osm.DefaultFilter()
	tests := []struct {
		name     string
		tags     map[string]string
		expected bool
	}{
		{"footway", map[string]string{"highway": "footway"}, true},
		{"path", map[string]string{"highway": "path"}, true},
		{"residential", map[string]string{"highway": "residential"}, true},
		{"motorway", map[string]string{"highway": "motorway"}, true},
		{"no foot footway", map[string]string{"highway": "footway", "foot": "no"}, false},
		{"no foot residential still routable for car", map[string]string{"highway": "residential", "foot": "no"}, true},
		{"private access", map[string]string{"highway": "path", "access": "private"}, false},
		{"no access", map[string]string{"highway": "residential", "access": "no"}, false},
		{"customers access", map[string]string{"highway": "service", "access": "customers"}, false},
		{"permit motor vehicle", map[string]string{"highway": "service", "motor_vehicle": "permit"}, false},
		{"parking aisle", map[string]string{"highway": "service", "service": "parking_aisle"}, false},
		{"driveway", map[string]string{"highway": "service", "service": "driveway"}, false},
		{"driveway with explicit foot access", map[string]string{"highway": "service", "service": "driveway", "foot": "yes"}, true},
		{"no highway tag", map[string]string{"name": "test"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &osm.Way{Tags: tt.tags}
			got := filter.IsWalkable(w)
			if got != tt.expected {
				t.Errorf("IsWalkable(%v) = %v, want %v", tt.tags, got, tt.expected)
			}
		})
	}
}

func TestFilterWays(t *testing.T) {
	data, err := osm.ParseXML(strings.NewReader(testOSMXML))
	if err != nil {
		t.Fatalf("ParseXML() error = %v", err)
	}

	filter := osm.DefaultFilter()
	walkable := data.FilterWays(filter)

	// The default graph is a public routable union graph, not walking-only.
	// It includes walkable ways plus public car-only ways with walking restrictions.
	if len(walkable) != 3 {
		t.Errorf("expected 3 routable ways, got %d", len(walkable))
	}
}

func TestBuildGraph(t *testing.T) {
	data, err := osm.ParseXML(strings.NewReader(testOSMXML))
	if err != nil {
		t.Fatalf("ParseXML() error = %v", err)
	}

	g := osm.BuildGraph(data, nil)

	// Should have nodes 1, 2, 3 from the walkable footway
	if !g.HasNode(1) || !g.HasNode(2) || !g.HasNode(3) {
		t.Error("graph should have nodes 1, 2, 3")
	}

	// Node 4 is included because the graph is a mode-restricted union graph.
	if !g.HasNode(4) {
		t.Error("graph should have node 4")
	}

	// Check edges exist
	neighbors1 := g.Neighbors(1)
	if len(neighbors1) != 1 {
		t.Errorf("node 1 should have 1 neighbor, got %d", len(neighbors1))
	}

	neighbors2 := g.Neighbors(2)
	if len(neighbors2) != 3 {
		t.Errorf("node 2 should have 3 neighbors (1, 3, and 4), got %d", len(neighbors2))
	}
}

func TestBuildGraphRestrictsWalkingOnCarOnlyHighways(t *testing.T) {
	data, err := osm.ParseXML(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<osm version="0.6">
  <node id="1" lat="0" lon="0"/>
  <node id="2" lat="0" lon="0.001"/>
  <way id="10">
    <nd ref="1"/>
    <nd ref="2"/>
    <tag k="highway" v="motorway"/>
  </way>
</osm>`))
	if err != nil {
		t.Fatalf("ParseXML() error = %v", err)
	}

	g := osm.BuildGraph(data, nil)
	edges := g.Neighbors(1)
	if len(edges) != 1 || edges[0].To != 2 {
		t.Fatalf("expected motorway edge in graph, got %+v", edges)
	}
	if len(edges[0].RestrictedModes) != 1 || edges[0].RestrictedModes[0] != graph.RestrictedWalking {
		t.Fatalf("expected motorway to restrict walking, got %+v", edges[0].RestrictedModes)
	}
}

func TestBuildGraphRestrictsDrivingOnNonCarHighways(t *testing.T) {
	data, err := osm.ParseXML(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<osm version="0.6">
  <node id="1" lat="0" lon="0"/>
  <node id="2" lat="0" lon="0.001"/>
  <way id="10">
    <nd ref="1"/>
    <nd ref="2"/>
    <tag k="highway" v="footway"/>
  </way>
</osm>`))
	if err != nil {
		t.Fatalf("ParseXML() error = %v", err)
	}

	g := osm.BuildGraph(data, nil)

	assertEdgeDirection(t, g.Neighbors(1), 2, true, true)
	assertEdgeDirection(t, g.Neighbors(2), 1, true, true)
}

func TestBuildGraphHonorsOnewayDirection(t *testing.T) {
	tests := []struct {
		name              string
		tags              string
		forwardOpen       bool
		reverseOpen       bool
		forwardRestricted bool
		reverseRestricted bool
	}{
		{name: "oneway yes", tags: `<tag k="oneway" v="yes"/>`, forwardOpen: true, reverseOpen: true, reverseRestricted: true},
		{name: "oneway true", tags: `<tag k="oneway" v="true"/>`, forwardOpen: true, reverseOpen: true, reverseRestricted: true},
		{name: "oneway 1", tags: `<tag k="oneway" v="1"/>`, forwardOpen: true, reverseOpen: true, reverseRestricted: true},
		{name: "oneway reverse", tags: `<tag k="oneway" v="-1"/>`, forwardOpen: true, reverseOpen: true, forwardRestricted: true},
		{name: "oneway reverse word", tags: `<tag k="oneway" v="reverse"/>`, forwardOpen: true, reverseOpen: true, forwardRestricted: true},
		{name: "roundabout implied", tags: `<tag k="junction" v="roundabout"/>`, forwardOpen: true, reverseOpen: true, reverseRestricted: true},
		{name: "explicit not oneway", tags: `<tag k="oneway" v="no"/>`, forwardOpen: true, reverseOpen: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := osm.ParseXML(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<osm version="0.6">
  <node id="1" lat="0" lon="0"/>
  <node id="2" lat="0" lon="0.001"/>
  <way id="10">
    <nd ref="1"/>
    <nd ref="2"/>
    <tag k="highway" v="residential"/>` + tt.tags + `
  </way>
</osm>`))
			if err != nil {
				t.Fatalf("ParseXML() error = %v", err)
			}

			g := osm.BuildGraph(data, nil)
			assertEdgeDirection(t, g.Neighbors(1), 2, tt.forwardOpen, tt.forwardRestricted)
			assertEdgeDirection(t, g.Neighbors(2), 1, tt.reverseOpen, tt.reverseRestricted)
		})
	}
}

func assertEdgeDirection(t *testing.T, edges []graph.Edge, to graph.NodeID, wantOpen, wantRestricted bool) {
	t.Helper()
	for _, edge := range edges {
		if edge.To != to {
			continue
		}
		if !wantOpen {
			t.Fatalf("unexpected edge to %d in %+v", to, edges)
		}
		gotRestricted := len(edge.RestrictedModes) == 1 && edge.RestrictedModes[0] == graph.RestrictedDriving
		if gotRestricted != wantRestricted {
			t.Fatalf("edge restriction to %d = %v, want %v", to, edge.RestrictedModes, wantRestricted)
		}
		return
	}
	if wantOpen {
		t.Fatalf("missing edge to %d in %+v", to, edges)
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
