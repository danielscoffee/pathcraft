package osm

var WalkableHighways = map[string]bool{
	"footway":        true,
	"path":           true,
	"pedestrian":     true,
	"steps":          true,
	"cycleway":       true,
	"residential":    true,
	"living_street":  true,
	"service":        true,
	"track":          true,
	"unclassified":   true,
	"tertiary":       true,
	"tertiary_link":  true,
	"secondary":      true,
	"secondary_link": true,
	"primary":        true,
	"primary_link":   true,
	"trunk":          true,
	"trunk_link":     true,
	"motorway":       true,
	"motorway_link":  true,
}

type Filter struct {
	IncludeHighways map[string]bool
}

func DefaultFilter() *Filter {
	return &Filter{
		IncludeHighways: WalkableHighways,
	}
}

func (f *Filter) IsWalkable(w *Way) bool {
	policy := PolicyForTags(w.Tags)
	highways := f.IncludeHighways
	if highways == nil {
		highways = WalkableHighways
	}
	return routableTags(w.Tags, highways, policy.RestrictDriving)
}

func (d *Data) FilterWays(f *Filter) []*Way {
	var result []*Way
	for _, w := range d.Ways {
		if f.IsWalkable(w) {
			result = append(result, w)
		}
	}
	return result
}
