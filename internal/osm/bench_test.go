package osm

import (
	"os"
	"testing"
)

// BenchmarkParseFile measures OSM XML parse time from examples/example.osm.
// NOTE: this is a toy fixture (~6 nodes). Numbers are NOT representative of
// city-scale extracts — they only provide a baseline for the parser's
// per-byte overhead on small inputs.
func BenchmarkParseFile_ExampleOSM(b *testing.B) {
	const path = "../../examples/example.osm"
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatalf("read %s: %v", path, err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		d, err := ParseFile(path)
		if err != nil {
			b.Fatalf("ParseFile: %v", err)
		}
		if d == nil {
			b.Fatal("nil data")
		}
	}
}

// BenchmarkBuildGraph_ExampleOSM measures the cost of turning parsed OSM
// data into a walkable graph, excluding the parse step.
func BenchmarkBuildGraph_ExampleOSM(b *testing.B) {
	const path = "../../examples/example.osm"
	data, err := ParseFile(path)
	if err != nil {
		b.Fatalf("ParseFile: %v", err)
	}
	filter := DefaultFilter()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g := BuildGraph(data, filter)
		if g == nil {
			b.Fatal("nil graph")
		}
	}
}
