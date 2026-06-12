package gtfs

import (
	"os"
	"testing"
)

// BenchmarkParseStopTimes_ExampleGTFS measures stop_times.txt parse time on
// the small Recife example fixture (~102 stop_times, 16 trips). Numbers
// here are a lower bound — real GTFS feeds have O(10^5-10^6) stop_times.
func BenchmarkParseStopTimes_ExampleGTFS(b *testing.B) {
	const path = "../../testdata/mini_gtfs/stop_times.txt"
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatalf("read %s: %v", path, err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		st, err := ParseStopTimesFile(path)
		if err != nil {
			b.Fatalf("ParseStopTimesFile: %v", err)
		}
		if len(st) == 0 {
			b.Fatal("empty stop_times")
		}
	}
}

// BenchmarkBuildIndex_ExampleGTFS measures the RAPTOR index build cost on
// the example fixture (parse excluded).
func BenchmarkBuildIndex_ExampleGTFS(b *testing.B) {
	stopTimes, err := ParseStopTimesFile("../../testdata/mini_gtfs/stop_times.txt")
	if err != nil {
		b.Fatalf("parse stop_times: %v", err)
	}
	tripRoutes, err := ParseTripsFile("../../testdata/mini_gtfs/trips.txt")
	if err != nil {
		b.Fatalf("parse trips: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := BuildIndex(stopTimes, tripRoutes)
		if idx == nil {
			b.Fatal("nil index")
		}
	}
}
