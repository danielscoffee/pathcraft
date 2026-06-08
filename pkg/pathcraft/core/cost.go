package core

// RouteState is the running state passed to a CostModel when evaluating an
// edge during search. Algorithms that support custom costs feed this in.
type RouteState struct {
	ElapsedSec float64
	Hops       int
	Profile    string
}

// CostModel re-weights edges for a given routing profile (walking, cycling,
// driving, fastest, shortest, ...). Returning math.Inf(1) excludes the edge.
type CostModel interface {
	Name() string
	Cost(edge Edge, state RouteState) float64
}
