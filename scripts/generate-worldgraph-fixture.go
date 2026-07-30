// Command generate-worldgraph-fixture writes PathCraft's deterministic synthetic
// OSM PBF seam fixture. Data is generated locally and contains no imported OSM data.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"google.golang.org/protobuf/encoding/protowire"
)

var output = flag.String("output", "pkg/plugins/worldgraph/builder/testdata/seam.osm.pbf", "fixture output path")

type fixtureNode struct {
	id       int64
	lon, lat float64
}

type fixtureWay struct {
	id      int64
	nodes   []int64
	highway string
	name    string
}

func main() {
	flag.Parse()
	data := fixturePBF()
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(*output, data, 0o600); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", *output, len(data))
}

func fixturePBF() []byte {
	nodes := []fixtureNode{
		{id: 1, lon: 12.5683, lat: 55.6761},
		{id: 2, lon: 12.5684, lat: 55.6761},
		{id: 3, lon: 12.5685, lat: 55.6762},
	}
	ways := []fixtureWay{
		{id: 100, nodes: []int64{1, 2, 3}, highway: "residential", name: "Seam Street"},
		{id: 101, nodes: []int64{3, 2}, highway: "footway", name: "Foot Link"},
	}

	header := fieldString(nil, 4, "OsmSchema-V0.6")
	header = fieldString(header, 4, "DenseNodes")
	header = fieldString(header, 16, "PathCraft deterministic fixture generator")
	header = fieldString(header, 17, "Synthetic zoom-12 seam fixture")

	strings := []string{"", "highway", "residential", "name", "Seam Street", "footway", "Foot Link"}
	var stringTable []byte
	for _, value := range strings {
		stringTable = fieldString(stringTable, 1, value)
	}

	var ids, lats, lons []int64
	var previousID, previousLat, previousLon int64
	for _, node := range nodes {
		lat := coordinate(node.lat)
		lon := coordinate(node.lon)
		ids = append(ids, node.id-previousID)
		lats = append(lats, lat-previousLat)
		lons = append(lons, lon-previousLon)
		previousID, previousLat, previousLon = node.id, lat, lon
	}
	var dense []byte
	dense = fieldBytes(dense, 1, packedSInt64(ids))
	dense = fieldBytes(dense, 8, packedSInt64(lats))
	dense = fieldBytes(dense, 9, packedSInt64(lons))
	nodeGroup := fieldBytes(nil, 2, dense)

	stringID := map[string]uint64{
		"highway":     1,
		"residential": 2,
		"name":        3,
		"Seam Street": 4,
		"footway":     5,
		"Foot Link":   6,
	}
	var wayGroup []byte
	for _, way := range ways {
		keys := []uint64{stringID["highway"], stringID["name"]}
		values := []uint64{stringID[way.highway], stringID[way.name]}
		var refs []int64
		var previous int64
		for _, id := range way.nodes {
			refs = append(refs, id-previous)
			previous = id
		}
		encoded := fieldVarint(nil, 1, uint64(way.id))
		encoded = fieldBytes(encoded, 2, packedVarints(keys))
		encoded = fieldBytes(encoded, 3, packedVarints(values))
		encoded = fieldBytes(encoded, 8, packedSInt64(refs))
		wayGroup = fieldBytes(wayGroup, 3, encoded)
	}

	block := fieldBytes(nil, 1, stringTable)
	block = fieldBytes(block, 2, nodeGroup)
	block = fieldBytes(block, 2, wayGroup)
	return append(fileBlock("OSMHeader", header), fileBlock("OSMData", block)...)
}

func coordinate(value float64) int64 {
	return int64(math.Round(value * 1e7))
}

func fileBlock(kind string, payload []byte) []byte {
	blob := fieldBytes(nil, 1, payload)
	blob = fieldVarint(blob, 2, uint64(len(payload)))
	header := fieldString(nil, 1, kind)
	header = fieldVarint(header, 3, uint64(len(blob)))
	result := make([]byte, 4)
	binary.BigEndian.PutUint32(result, uint32(len(header)))
	result = append(result, header...)
	return append(result, blob...)
}

func fieldString(dst []byte, number protowire.Number, value string) []byte {
	return fieldBytes(dst, number, []byte(value))
}

func fieldBytes(dst []byte, number protowire.Number, value []byte) []byte {
	dst = protowire.AppendTag(dst, number, protowire.BytesType)
	return protowire.AppendBytes(dst, value)
}

func fieldVarint(dst []byte, number protowire.Number, value uint64) []byte {
	dst = protowire.AppendTag(dst, number, protowire.VarintType)
	return protowire.AppendVarint(dst, value)
}

func packedVarints(values []uint64) []byte {
	var result []byte
	for _, value := range values {
		result = protowire.AppendVarint(result, value)
	}
	return result
}

func packedSInt64(values []int64) []byte {
	var result []byte
	for _, value := range values {
		result = protowire.AppendVarint(result, protowire.EncodeZigZag(value))
	}
	return result
}
