package osm

import "strings"

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
	if restrictedAccessValue(w.Tags["access"]) {
		return false
	}

	highway := w.Tags["highway"]
	if highway == "" {
		return false
	}

	highways := f.IncludeHighways
	if highways == nil {
		highways = WalkableHighways
	}
	if !highways[highway] {
		return false
	}

	if explicitlyDenied(w.Tags["foot"]) && drivingRestricted(w) {
		return false
	}
	if restrictedAccessValue(w.Tags["motor_vehicle"]) && !explicitlyAllowed(w.Tags["foot"]) {
		return false
	}

	if restrictedService(w) && !explicitlyAllowed(w.Tags["foot"]) {
		return false
	}

	return true
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

func explicitlyDenied(value string) bool {
	switch normalizedTagValue(value) {
	case "no", "private", "customers", "permit", "delivery", "unknown", "não", "nao":
		return true
	default:
		return false
	}
}

func restrictedAccessValue(value string) bool {
	return explicitlyDenied(value)
}

func explicitlyAllowed(value string) bool {
	switch normalizedTagValue(value) {
	case "yes", "designated", "permissive", "sim":
		return true
	default:
		return false
	}
}

func restrictedService(w *Way) bool {
	if w.Tags["highway"] != "service" {
		return false
	}
	switch normalizedTagValue(w.Tags["service"]) {
	case "parking_aisle", "driveway", "drive-through", "emergency_access", "yard":
		return true
	default:
		return false
	}
}

func drivingRestricted(w *Way) bool {
	if explicitlyDenied(w.Tags["vehicle"]) || explicitlyDenied(w.Tags["motor_vehicle"]) || explicitlyDenied(w.Tags["motorcar"]) {
		return true
	}
	if restrictedService(w) {
		return true
	}
	switch normalizedTagValue(w.Tags["highway"]) {
	case "footway", "path", "pedestrian", "steps", "cycleway", "corridor", "bus_stop", "busway", "construction":
		return true
	default:
		return false
	}
}

func walkingRestricted(w *Way) bool {
	if explicitlyDenied(w.Tags["foot"]) {
		return true
	}
	if explicitlyAllowed(w.Tags["foot"]) {
		return false
	}
	switch normalizedTagValue(w.Tags["highway"]) {
	case "motorway", "motorway_link", "trunk", "trunk_link":
		return true
	default:
		return false
	}
}

func normalizedTagValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
