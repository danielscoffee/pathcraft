package graph

type ViewModel struct {
	Nodes []Node `json:"nodes"`
	Meta  Meta   `json:"meta"`
}

type Meta struct {
	NodeCount int `json:"node_count"`
	EdgeCount int `json:"edge_count"`
}

func Build(g *Graph) ViewModel {
	nodes := make([]Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes = append(nodes, Node{
			ID:  n.ID,
			Lat: n.Lat,
			Lon: n.Lon,
		})
	}

	return ViewModel{
		Nodes: nodes,
		Meta: Meta{
			NodeCount: len(g.Nodes),
			EdgeCount: len(g.Edges),
		},
	}
}
