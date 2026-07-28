package osm_test

import (
	"testing"

	"github.com/danielscoffee/pathcraft/internal/osm"
)

func TestFilterPolicyPreservesCustomHighways(t *testing.T) {
	filter := &osm.Filter{IncludeHighways: map[string]bool{"corridor": true}}
	if !filter.IsWalkable(&osm.Way{Tags: map[string]string{"highway": "corridor"}}) {
		t.Fatal("custom corridor highway rejected")
	}
}

func TestPolicyForTags(t *testing.T) {
	tests := []struct {
		name string
		tags map[string]string
		want osm.WayPolicy
	}{
		{
			name: "residential",
			tags: map[string]string{"highway": "residential"},
			want: osm.WayPolicy{Routable: true},
		},
		{
			name: "footway restricts driving",
			tags: map[string]string{"highway": "footway"},
			want: osm.WayPolicy{Routable: true, RestrictDriving: true},
		},
		{
			name: "motorway restricts walking",
			tags: map[string]string{"highway": "motorway"},
			want: osm.WayPolicy{Routable: true, RestrictWalking: true},
		},
		{
			name: "private access",
			tags: map[string]string{"highway": "residential", "access": "private"},
			want: osm.WayPolicy{},
		},
		{
			name: "foot override keeps car-restricted way routable",
			tags: map[string]string{"highway": "residential", "motor_vehicle": "no", "foot": "yes"},
			want: osm.WayPolicy{Routable: true, RestrictDriving: true},
		},
		{
			name: "car access keeps foot-restricted way routable",
			tags: map[string]string{"highway": "residential", "foot": "no", "motor_vehicle": "yes"},
			want: osm.WayPolicy{Routable: true, RestrictWalking: true},
		},
		{
			name: "restricted service",
			tags: map[string]string{"highway": "service", "service": "driveway"},
			want: osm.WayPolicy{RestrictDriving: true},
		},
		{
			name: "restricted service foot override",
			tags: map[string]string{"highway": "service", "service": "driveway", "foot": "designated"},
			want: osm.WayPolicy{Routable: true, RestrictDriving: true},
		},
		{
			name: "oneway forward",
			tags: map[string]string{"highway": "residential", "oneway": "yes"},
			want: osm.WayPolicy{Routable: true, Direction: osm.DirectionForward},
		},
		{
			name: "oneway reverse",
			tags: map[string]string{"highway": "residential", "oneway": "-1"},
			want: osm.WayPolicy{Routable: true, Direction: osm.DirectionReverse},
		},
		{
			name: "roundabout defaults forward",
			tags: map[string]string{"highway": "residential", "junction": "roundabout"},
			want: osm.WayPolicy{Routable: true, Direction: osm.DirectionForward},
		},
		{
			name: "explicit two-way overrides roundabout default",
			tags: map[string]string{"highway": "residential", "junction": "roundabout", "oneway": "no"},
			want: osm.WayPolicy{Routable: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := osm.PolicyForTags(test.tags); got != test.want {
				t.Fatalf("PolicyForTags(%v) = %+v, want %+v", test.tags, got, test.want)
			}
		})
	}
}
