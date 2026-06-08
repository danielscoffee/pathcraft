// Package core defines the public interfaces and value types of PathCraft.
//
// It is intentionally small: a few interfaces (Graph, Algorithm, GraphLoader,
// Exporter, CostModel) and the value types they exchange (NodeID, Edge,
// RouteRequest, RouteResult). Concrete implementations live in
// github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/* and register
// themselves with pkg/pathcraft/registry.
package core
