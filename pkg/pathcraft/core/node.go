package core

// NodeID identifies a node in a Graph. Plugins decide the encoding:
// OSM uses int64 stringified, GTFS uses stop_id as-is.
type NodeID string
