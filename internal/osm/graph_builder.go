package osm

import (
	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/graph"
)

func BuildGraph(data *Data, filter *Filter) *graph.Graph {
	if filter == nil {
		filter = DefaultFilter()
	}

	g := graph.NewGraph()

	walkableWays := data.FilterWays(filter)

	referencedNodes := make(map[int64]bool)
	for _, w := range walkableWays {
		for _, nodeID := range w.NodeIDs {
			referencedNodes[nodeID] = true
		}
	}

	for nodeID := range referencedNodes {
		node, ok := data.Nodes[nodeID]
		if !ok {
			continue
		}
		g.AddNode(graph.NodeID(nodeID), node.Lat, node.Lon)
	}

	for _, w := range walkableWays {
		direction := drivingDirection(w)

		for i := 0; i < len(w.NodeIDs)-1; i++ {
			fromID := w.NodeIDs[i]
			toID := w.NodeIDs[i+1]

			fromNode, okFrom := data.Nodes[fromID]
			toNode, okTo := data.Nodes[toID]
			if !okFrom || !okTo {
				continue
			}

			distance := geo.HaversineDistance(fromNode.Lat, fromNode.Lon, toNode.Lat, toNode.Lon)

			highway := w.Tags["highway"]
			name := w.Tags["name"]
			drivingRestricted := drivingRestricted(w)
			walkingRestricted := walkingRestricted(w)
			switch direction {
			case onewayForward:
				addEdgeWithRestrictions(g, graph.NodeID(fromID), graph.NodeID(toID), distance, highway, name, drivingRestricted, walkingRestricted)
				g.AddRestrictedEdgeWithMeta(graph.NodeID(toID), graph.NodeID(fromID), distance, highway, name, graph.RestrictedDriving)
			case onewayReverse:
				g.AddRestrictedEdgeWithMeta(graph.NodeID(fromID), graph.NodeID(toID), distance, highway, name, graph.RestrictedDriving)
				addEdgeWithRestrictions(g, graph.NodeID(toID), graph.NodeID(fromID), distance, highway, name, drivingRestricted, walkingRestricted)
			default:
				addEdgeWithRestrictions(g, graph.NodeID(fromID), graph.NodeID(toID), distance, highway, name, drivingRestricted, walkingRestricted)
				addEdgeWithRestrictions(g, graph.NodeID(toID), graph.NodeID(fromID), distance, highway, name, drivingRestricted, walkingRestricted)
			}
		}
	}

	return g
}

func addEdgeWithRestrictions(g *graph.Graph, from, to graph.NodeID, distance float64, highway, name string, restrictDriving, restrictWalking bool) {
	restrictedModes := make([]graph.RestrictedMode, 0, 2)
	if restrictDriving {
		restrictedModes = append(restrictedModes, graph.RestrictedDriving)
	}
	if restrictWalking {
		restrictedModes = append(restrictedModes, graph.RestrictedWalking)
	}
	g.AddRestrictedEdgeWithMeta(from, to, distance, highway, name, restrictedModes...)
}

type onewayDirection int

const (
	twoway onewayDirection = iota
	onewayForward
	onewayReverse
)

func drivingDirection(w *Way) onewayDirection {
	switch PolicyForTags(w.Tags).Direction {
	case DirectionForward:
		return onewayForward
	case DirectionReverse:
		return onewayReverse
	default:
		return twoway
	}
}

func drivingRestricted(w *Way) bool {
	return PolicyForTags(w.Tags).RestrictDriving
}

func walkingRestricted(w *Way) bool {
	return PolicyForTags(w.Tags).RestrictWalking
}
