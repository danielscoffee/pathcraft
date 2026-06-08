package core

import "context"

// Algorithm is a pluggable pathfinding strategy (A*, Dijkstra, RAPTOR, ...).
// Implementations register themselves with pkg/pathcraft/registry via init().
type Algorithm interface {
	Name() string
	Route(ctx context.Context, g Graph, req RouteRequest) (RouteResult, error)
}
