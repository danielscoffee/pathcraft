package gtfs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const chunkedStopTimesCSV = `trip_id,arrival_time,departure_time,stop_id,stop_sequence
T1,05:00:00,05:00:00,A,1
T1,05:05:00,05:05:00,B,2
T1,05:10:00,05:10:00,C,3
T2,06:00:00,06:00:00,A,1
T2,06:05:00,06:05:00,B,2
`

func TestParseStopTimesChunks_DeliversAllRowsInOrder(t *testing.T) {
	var chunks [][]StopTime
	total, err := ParseStopTimesChunks(strings.NewReader(chunkedStopTimesCSV), 2, func(chunk []StopTime) error {
		copied := make([]StopTime, len(chunk))
		copy(copied, chunk)
		chunks = append(chunks, copied)
		return nil
	})
	if err != nil {
		t.Fatalf("ParseStopTimesChunks: %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d; want 5", total)
	}
	if len(chunks) != 3 || len(chunks[0]) != 2 || len(chunks[1]) != 2 || len(chunks[2]) != 1 {
		t.Fatalf("chunk sizes wrong: %d chunks", len(chunks))
	}

	var streamed []StopTime
	for _, c := range chunks {
		streamed = append(streamed, c...)
	}
	direct, err := ParseStopTimes(strings.NewReader(chunkedStopTimesCSV))
	if err != nil {
		t.Fatalf("ParseStopTimes: %v", err)
	}
	if !reflect.DeepEqual(streamed, direct) {
		t.Fatalf("streamed rows differ from direct parse\nstreamed: %+v\ndirect: %+v", streamed, direct)
	}
}

func TestParseStopTimesChunks_PropagatesCallbackError(t *testing.T) {
	boom := errors.New("boom")
	_, err := ParseStopTimesChunks(strings.NewReader(chunkedStopTimesCSV), 1, func([]StopTime) error {
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want wrapped boom", err)
	}
}

func TestIndexBuilder_MatchesBuildIndex(t *testing.T) {
	stopTimes, err := ParseStopTimes(strings.NewReader(chunkedStopTimesCSV))
	if err != nil {
		t.Fatalf("ParseStopTimes: %v", err)
	}
	tripRoutes := TripToRoute{"T1": "R1", "T2": "R1"}

	want := BuildIndex(stopTimes, tripRoutes)

	builder := NewIndexBuilder()
	builder.Add(stopTimes[:2])
	builder.Add(stopTimes[2:])
	got := builder.Build(tripRoutes)

	if !reflect.DeepEqual(got.RoutePatterns, want.RoutePatterns) {
		t.Fatalf("RoutePatterns differ:\ngot %+v\nwant %+v", got.RoutePatterns, want.RoutePatterns)
	}
	if !reflect.DeepEqual(got.RouteTrips, want.RouteTrips) {
		t.Fatalf("RouteTrips differ")
	}
	if !reflect.DeepEqual(got.StopRoutes, want.StopRoutes) {
		t.Fatalf("StopRoutes differ")
	}
}

func TestLoadStopTimesChunked_ReportsProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stop_times.txt")
	if err := os.WriteFile(path, []byte(chunkedStopTimesCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	var reports []int
	builder, total, err := LoadStopTimesChunked(path, 2, func(rows int) { reports = append(reports, rows) })
	if err != nil {
		t.Fatalf("LoadStopTimesChunked: %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d; want 5", total)
	}
	if len(reports) != 3 || reports[len(reports)-1] != 5 {
		t.Fatalf("progress reports = %v; want cumulative ending at 5", reports)
	}

	idx := builder.Build(TripToRoute{"T1": "R1", "T2": "R1"})
	if len(idx.RoutePatterns) == 0 {
		t.Fatal("expected route patterns from chunked load")
	}
}

const chunkedShapesCSV = `shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence
S1,-8.05,-34.88,2
S1,-8.04,-34.87,1
S2,-8.06,-34.89,1
`

func TestLoadShapesChunked_MatchesParseShapes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shapes.txt")
	if err := os.WriteFile(path, []byte(chunkedShapesCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	want, err := ParseShapesFile(path)
	if err != nil {
		t.Fatalf("ParseShapesFile: %v", err)
	}

	var reports []int
	got, err := LoadShapesChunked(path, 2, func(points int) { reports = append(reports, points) })
	if err != nil {
		t.Fatalf("LoadShapesChunked: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shapes differ:\ngot %+v\nwant %+v", got, want)
	}
	if len(reports) == 0 || reports[len(reports)-1] != 3 {
		t.Fatalf("progress reports = %v; want cumulative ending at 3", reports)
	}
}
