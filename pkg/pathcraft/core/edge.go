package core

// Edge is a directed connection between two nodes with a scalar Cost.
// Algorithms minimize sum(Cost). Meta carries plugin-specific attributes
// (mode, trip_id, geometry, etc.) and is opaque to the core.
type Edge struct {
	From NodeID
	To   NodeID
	Cost float64
	Meta map[string]any
}
