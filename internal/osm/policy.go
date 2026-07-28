package osm

import "strings"

type Direction uint8

const (
	DirectionBoth Direction = iota
	DirectionForward
	DirectionReverse
)

type WayPolicy struct {
	Routable        bool
	Direction       Direction
	RestrictWalking bool
	RestrictDriving bool
}

func PolicyForTags(tags map[string]string) WayPolicy {
	policy := WayPolicy{
		Direction:       directionForTags(tags),
		RestrictWalking: walkingRestrictedForTags(tags),
		RestrictDriving: drivingRestrictedForTags(tags),
	}
	policy.Routable = routableTags(tags, WalkableHighways, policy.RestrictDriving)
	return policy
}

func routableTags(tags map[string]string, highways map[string]bool, restrictDriving bool) bool {
	if restrictedAccessValue(tags["access"]) {
		return false
	}

	highway := tags["highway"]
	if highway == "" || !highways[highway] {
		return false
	}
	if explicitlyDenied(tags["foot"]) && restrictDriving {
		return false
	}
	if restrictedAccessValue(tags["motor_vehicle"]) && !explicitlyAllowed(tags["foot"]) {
		return false
	}
	if restrictedServiceTags(tags) && !explicitlyAllowed(tags["foot"]) {
		return false
	}
	return true
}

func directionForTags(tags map[string]string) Direction {
	switch normalizedTagValue(tags["oneway"]) {
	case "yes", "true", "1":
		return DirectionForward
	case "-1", "reverse":
		return DirectionReverse
	case "no", "false", "0":
		return DirectionBoth
	}
	if normalizedTagValue(tags["junction"]) == "roundabout" {
		return DirectionForward
	}
	return DirectionBoth
}

func drivingRestrictedForTags(tags map[string]string) bool {
	if explicitlyDenied(tags["vehicle"]) || explicitlyDenied(tags["motor_vehicle"]) || explicitlyDenied(tags["motorcar"]) {
		return true
	}
	if restrictedServiceTags(tags) {
		return true
	}
	switch normalizedTagValue(tags["highway"]) {
	case "footway", "path", "pedestrian", "steps", "cycleway", "corridor", "bus_stop", "busway", "construction":
		return true
	default:
		return false
	}
}

func walkingRestrictedForTags(tags map[string]string) bool {
	if explicitlyDenied(tags["foot"]) {
		return true
	}
	if explicitlyAllowed(tags["foot"]) {
		return false
	}
	switch normalizedTagValue(tags["highway"]) {
	case "motorway", "motorway_link", "trunk", "trunk_link":
		return true
	default:
		return false
	}
}

func restrictedServiceTags(tags map[string]string) bool {
	if tags["highway"] != "service" {
		return false
	}
	switch normalizedTagValue(tags["service"]) {
	case "parking_aisle", "driveway", "drive-through", "emergency_access", "yard":
		return true
	default:
		return false
	}
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

func normalizedTagValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
