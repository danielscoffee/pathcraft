package engine

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/mobility"
)

func TestNewWithConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{name: "mode", config: Config{Mode: "plane"}},
		{name: "negative speed", config: Config{SpeedMPS: -1}},
		{name: "non-finite speed", config: Config{SpeedMPS: math.Inf(1)}},
		{name: "empty highway", config: Config{HighwayPenalties: map[string]float64{" ": 2}}},
		{name: "discount penalty", config: Config{HighwayPenalties: map[string]float64{"primary": 0.5}}},
		{name: "non-finite penalty", config: Config{HighwayPenalties: map[string]float64{"primary": math.NaN()}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewWithConfig(test.config); err == nil {
				t.Fatalf("NewWithConfig(%+v) error = nil", test.config)
			}
		})
	}
}

func TestConfigUsesModeDefaultSpeed(t *testing.T) {
	tests := []struct {
		mode        Mode
		wantSpeed   float64
		wantProfile string
	}{
		{mode: ModeWalk, wantSpeed: mobility.DefaultWalkingSpeedMPS, wantProfile: "walking"},
		{mode: ModeBike, wantSpeed: defaultBikeSpeedMPS, wantProfile: "walking"},
		{mode: ModeCar, wantSpeed: mobility.DefaultDrivingSpeedMPS, wantProfile: "driving"},
	}

	for _, test := range tests {
		t.Run(string(test.mode), func(t *testing.T) {
			e, err := NewWithConfig(Config{Mode: test.mode})
			if err != nil {
				t.Fatalf("NewWithConfig() error = %v", err)
			}
			profile := e.routeProfile(nil)
			if profile.Speed() != test.wantSpeed || profile.Name() != test.wantProfile {
				t.Fatalf("profile = %s at %v m/s, want %s at %v", profile.Name(), profile.Speed(), test.wantProfile, test.wantSpeed)
			}
		})
	}
}

func TestConfiguredDefaultsRouteWithoutInternalProfile(t *testing.T) {
	e, err := NewWithConfig(Config{Mode: ModeWalk, SpeedMPS: 2})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	g := graph.NewGraph()
	g.AddNode(1, 0, 0)
	g.AddNode(2, 0, 0.001)
	g.AddEdgeWithMeta(1, 2, 100, "residential", "Main Street")
	e.graph = g

	res, err := e.Route(RouteRequest{From: 1, To: 2})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if res.Distance != 100 {
		t.Fatalf("Distance = %v, want 100", res.Distance)
	}
	if res.Duration != 50*time.Second {
		t.Fatalf("Duration = %v, want 50s", res.Duration)
	}
}

func TestConfiguredModeControlsAccess(t *testing.T) {
	e, err := NewWithConfig(Config{Mode: ModeCar})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	g := graph.NewGraph()
	g.AddNode(1, 0, 0)
	g.AddNode(2, 0, 0.001)
	g.AddRestrictedEdgeWithMeta(1, 2, 100, "footway", "Walk Only", graph.RestrictedDriving)
	e.graph = g

	if _, err := e.Route(RouteRequest{From: 1, To: 2}); err == nil || !strings.Contains(err.Error(), "no path") {
		t.Fatalf("Route() error = %v, want no path", err)
	}
}

func TestConfiguredHighwayPenaltyChangesRouteButNotDistance(t *testing.T) {
	penalties := map[string]float64{" PRIMARY ": 2}
	e, err := NewWithConfig(Config{
		Mode:             ModeWalk,
		SpeedMPS:         2,
		HighwayPenalties: penalties,
	})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	penalties[" PRIMARY "] = 1
	g := graph.NewGraph()
	for id := graph.NodeID(1); id <= 4; id++ {
		g.AddNode(id, 0, 0)
	}
	g.AddEdgeWithMeta(1, 2, 50, "primary", "Fast Road")
	g.AddEdgeWithMeta(2, 4, 50, "primary", "Fast Road")
	g.AddEdgeWithMeta(1, 3, 75, "residential", "Calm Road")
	g.AddEdgeWithMeta(3, 4, 75, "residential", "Calm Road")
	e.graph = g

	res, err := e.Route(RouteRequest{From: 1, To: 4})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	wantNodes := []int64{1, 3, 4}
	if len(res.Nodes) != len(wantNodes) {
		t.Fatalf("Nodes = %v, want %v", res.Nodes, wantNodes)
	}
	for i := range wantNodes {
		if res.Nodes[i] != wantNodes[i] {
			t.Fatalf("Nodes = %v, want %v", res.Nodes, wantNodes)
		}
	}
	if res.Distance != 150 {
		t.Fatalf("Distance = %v, want geometric distance 150", res.Distance)
	}
	if res.Duration != 75*time.Second {
		t.Fatalf("Duration = %v, want 75s", res.Duration)
	}

	overridden, err := e.Route(RouteRequest{From: 1, To: 4, Profile: mobility.NewWalking(3)})
	if err != nil {
		t.Fatalf("Route() with profile error = %v", err)
	}
	if overridden.Duration != 50*time.Second {
		t.Fatalf("override Duration = %v, want 50s", overridden.Duration)
	}
}

func TestConfiguredPenaltyInflatesDurationNotDistance(t *testing.T) {
	e, err := NewWithConfig(Config{SpeedMPS: 2, HighwayPenalties: map[string]float64{"primary": 2}})
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	g := graph.NewGraph()
	g.AddNode(1, 0, 0)
	g.AddNode(2, 0, 0)
	g.AddEdgeWithMeta(1, 2, 100, "primary", "Only Road")
	e.graph = g

	res, err := e.Route(RouteRequest{From: 1, To: 2})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if res.Distance != 100 || res.Duration != 100*time.Second {
		t.Fatalf("route = distance %v, duration %v; want 100m, 100s", res.Distance, res.Duration)
	}
}

func TestRouteRejectsNonFiniteDerivedValues(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{name: "duration", config: Config{SpeedMPS: math.SmallestNonzeroFloat64}},
		{name: "edge cost", config: Config{HighwayPenalties: map[string]float64{"primary": math.MaxFloat64}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e, err := NewWithConfig(test.config)
			if err != nil {
				t.Fatalf("NewWithConfig() error = %v", err)
			}
			g := graph.NewGraph()
			g.AddNode(1, 0, 0)
			g.AddNode(2, 0, 0)
			g.AddEdgeWithMeta(1, 2, 2, "primary", "Only Road")
			e.graph = g
			if _, err := e.Route(RouteRequest{From: 1, To: 2}); err == nil {
				t.Fatal("Route() error = nil, want derived-value rejection")
			}
		})
	}
}
