package core

// RouteRequest describes a routing query independent of any specific algorithm.
// Options carries algorithm-specific knobs (e.g. "departure_time" for RAPTOR,
// "speed_mps" for walking).
type RouteRequest struct {
	From      NodeID
	To        NodeID
	Algorithm string
	Profile   string
	Options   map[string]any
}

// RouteResult is the output of an Algorithm. Path is the ordered node
// sequence; Edges (optional) carries the resolved edges between them for
// exporters that need geometry.
type RouteResult struct {
	Path         []NodeID
	Edges        []Edge
	Cost         float64
	DurationMS   int64
	VisitedNodes int
	Meta         map[string]any
}
