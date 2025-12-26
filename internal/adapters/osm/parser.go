package osm

import (
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"

	geo "github.com/qoppatech/exp-pathcraft/internal/domain/geo"
	"github.com/qoppatech/exp-pathcraft/internal/domain/graph"
)

type Node struct {
	ID   int64
	Lat  float64
	Lon  float64
	Tags map[string]string
}

type Way struct {
	ID      int64
	NodeIDs []int64
	Tags    map[string]string
}

type Data struct {
	Nodes map[int64]*Node
	Ways  []*Way
}

func NewData() *Data {
	return &Data{
		Nodes: make(map[int64]*Node),
		Ways:  make([]*Way, 0),
	}
}

type xmlOSM struct {
	Nodes []xmlNode `xml:"node"`
	Ways  []xmlWay  `xml:"way"`
}

type xmlNode struct {
	ID   int64    `xml:"id,attr"`
	Lat  float64  `xml:"lat,attr"`
	Lon  float64  `xml:"lon,attr"`
	Tags []xmlTag `xml:"tag"`
}

type xmlWay struct {
	ID       int64    `xml:"id,attr"`
	NodeRefs []xmlNd  `xml:"nd"`
	Tags     []xmlTag `xml:"tag"`
}

type xmlNd struct {
	Ref int64 `xml:"ref,attr"`
}

type xmlTag struct {
	K string `xml:"k,attr"`
	V string `xml:"v,attr"`
}

func ParseXML(r io.Reader) (*Data, error) {
	var osm xmlOSM
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(&osm); err != nil {
		return nil, fmt.Errorf("decoding XML: %w", err)
	}

	data := NewData()

	for _, n := range osm.Nodes {
		tags := make(map[string]string)
		for _, t := range n.Tags {
			tags[t.K] = t.V
		}
		data.Nodes[n.ID] = &Node{
			ID:   n.ID,
			Lat:  n.Lat,
			Lon:  n.Lon,
			Tags: tags,
		}
	}

	for _, w := range osm.Ways {
		tags := make(map[string]string)
		for _, t := range w.Tags {
			tags[t.K] = t.V
		}

		nodeIDs := make([]int64, len(w.NodeRefs))
		for i, nd := range w.NodeRefs {
			nodeIDs[i] = nd.Ref
		}

		data.Ways = append(data.Ways, &Way{
			ID:      w.ID,
			NodeIDs: nodeIDs,
			Tags:    tags,
		})
	}

	return data, nil
}

func ParseFile(path string) (*Data, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var reader io.Reader = f

	if strings.HasSuffix(path, ".gz") {
		gzReader, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("opening gzip: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	return ParseXML(reader)
}

var WalkableHighways = map[string]bool{
	"footway":       true,
	"path":          true,
	"pedestrian":    true,
	"steps":         true,
	"residential":   true,
	"living_street": true,
	"service":       true,
	"track":         true,
	"unclassified":  true,
	"tertiary":      true,
	"secondary":     true,
	"primary":       true,
	"trunk":         true,
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
	if w.Tags["foot"] == "no" || w.Tags["access"] == "private" {
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

	return highways[highway]
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
		isOneway := w.Tags["oneway"] == "yes" || w.Tags["oneway"] == "1"
		_ = isOneway

		for i := 0; i < len(w.NodeIDs)-1; i++ {
			fromID := w.NodeIDs[i]
			toID := w.NodeIDs[i+1]

			fromNode, okFrom := data.Nodes[fromID]
			toNode, okTo := data.Nodes[toID]
			if !okFrom || !okTo {
				continue
			}

			distance := geo.HaversineDistance(fromNode.Lat, fromNode.Lon, toNode.Lat, toNode.Lon)

			g.AddBidirectionalEdge(graph.NodeID(fromID), graph.NodeID(toID), distance)
		}
	}

	return g
}
