package gtfs

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/danielscoffee/pathcraft/internal/time"
)

// City-scale feeds (Grande Recife: 3.1M stop_times rows, 180 MB) cannot be
// loaded as one giant slice without doubling peak memory. The chunked
// loaders below stream fixed-size batches into incremental consumers and
// intern repeated IDs so each unique trip/stop string is stored once.

// DefaultChunkSize balances callback overhead against batch memory.
const DefaultChunkSize = 100_000

// ParseStopTimesChunks streams stop_times rows to fn in chunks of chunkSize.
// The chunk slice is reused between calls — copy it if you need to keep it.
// Returns the total number of rows parsed.
func ParseStopTimesChunks(r io.Reader, chunkSize int, fn func(chunk []StopTime) error) (int, error) {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}

	csvReader := csv.NewReader(r)
	csvReader.ReuseRecord = true

	header, err := csvReader.Read()
	if err != nil {
		return 0, fmt.Errorf("reading header: %w", err)
	}
	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}
	for _, col := range []string{"trip_id", "stop_id", "arrival_time", "departure_time", "stop_sequence"} {
		if _, ok := colIndex[col]; !ok {
			return 0, fmt.Errorf("%w: %s", ErrMissingColumn, col)
		}
	}
	tripIdx := colIndex["trip_id"]
	stopIdx := colIndex["stop_id"]
	arrivalIdx := colIndex["arrival_time"]
	departureIdx := colIndex["departure_time"]
	seqIdx := colIndex["stop_sequence"]

	// Interning: a 3M-row feed repeats ~90k trip IDs and ~10k stop IDs.
	tripIntern := make(map[string]TripID)
	stopIntern := make(map[string]StopID)

	chunk := make([]StopTime, 0, chunkSize)
	total := 0
	lineNum := 1
	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		if err := fn(chunk); err != nil {
			return fmt.Errorf("chunk ending at line %d: %w", lineNum, err)
		}
		chunk = chunk[:0]
		return nil
	}

	for {
		lineNum++
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return total, fmt.Errorf("line %d: %w", lineNum, err)
		}

		arrivalTime, err := time.ParseTime(record[arrivalIdx])
		if err != nil {
			return total, fmt.Errorf("line %d: invalid arrival_time: %w", lineNum, err)
		}
		departureTime, err := time.ParseTime(record[departureIdx])
		if err != nil {
			return total, fmt.Errorf("line %d: invalid departure_time: %w", lineNum, err)
		}
		stopSequence, err := strconv.Atoi(strings.TrimSpace(record[seqIdx]))
		if err != nil {
			return total, fmt.Errorf("line %d: invalid stop_sequence: %w", lineNum, err)
		}

		tripKey := strings.TrimSpace(record[tripIdx])
		tripID, ok := tripIntern[tripKey]
		if !ok {
			tripID = TripID(strings.Clone(tripKey))
			tripIntern[string(tripID)] = tripID
		}
		stopKey := strings.TrimSpace(record[stopIdx])
		stopID, ok := stopIntern[stopKey]
		if !ok {
			stopID = StopID(strings.Clone(stopKey))
			stopIntern[string(stopID)] = stopID
		}

		chunk = append(chunk, StopTime{
			TripID:        tripID,
			StopID:        stopID,
			ArrivalTime:   arrivalTime,
			DepartureTime: departureTime,
			StopSequence:  stopSequence,
		})
		total++

		if len(chunk) == chunkSize {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}

	return total, flush()
}

// IndexBuilder accumulates stop_times chunks and produces the RAPTOR index
// without ever materializing the full row slice.
type IndexBuilder struct {
	tripStops map[TripID][]StopTime
}

func NewIndexBuilder() *IndexBuilder {
	return &IndexBuilder{tripStops: make(map[TripID][]StopTime)}
}

// Add groups a chunk by trip. Safe to call with a reused chunk slice:
// StopTime values are copied on append.
func (b *IndexBuilder) Add(chunk []StopTime) {
	for _, st := range chunk {
		b.tripStops[st.TripID] = append(b.tripStops[st.TripID], st)
	}
}

// Build finalizes the index. The builder must not be reused afterwards.
func (b *IndexBuilder) Build(tripRoutes TripToRoute) *StopTimeIndex {
	return buildIndexFromTripStops(b.tripStops, tripRoutes)
}

// LoadStopTimesChunked streams a stop_times.txt file into an IndexBuilder,
// invoking progress (if non-nil) with the cumulative row count per chunk.
func LoadStopTimesChunked(path string, chunkSize int, progress func(rows int)) (*IndexBuilder, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	builder := NewIndexBuilder()
	loaded := 0
	total, err := ParseStopTimesChunks(f, chunkSize, func(chunk []StopTime) error {
		builder.Add(chunk)
		loaded += len(chunk)
		if progress != nil {
			progress(loaded)
		}
		return nil
	})
	if err != nil {
		return nil, total, err
	}
	return builder, total, nil
}

// LoadShapesChunked streams shapes.txt, reporting cumulative point counts.
// Output is identical to ParseShapesFile (points sorted by sequence).
func LoadShapesChunked(path string, chunkSize int, progress func(points int)) (map[ShapeID][]ShapePoint, error) {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	csvReader := csv.NewReader(f)
	csvReader.ReuseRecord = true

	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}
	for _, col := range []string{"shape_id", "shape_pt_lat", "shape_pt_lon", "shape_pt_sequence"} {
		if _, ok := colIndex[col]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingColumn, col)
		}
	}
	idIdx := colIndex["shape_id"]
	latIdx := colIndex["shape_pt_lat"]
	lonIdx := colIndex["shape_pt_lon"]
	seqIdx := colIndex["shape_pt_sequence"]

	shapeIntern := make(map[string]ShapeID)
	shapes := make(map[ShapeID][]ShapePoint)
	loaded := 0
	sinceReport := 0
	lineNum := 1
	for {
		lineNum++
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		lat, err := strconv.ParseFloat(strings.TrimSpace(record[latIdx]), 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid shape_pt_lat: %w", lineNum, err)
		}
		lon, err := strconv.ParseFloat(strings.TrimSpace(record[lonIdx]), 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid shape_pt_lon: %w", lineNum, err)
		}
		seq, err := strconv.Atoi(strings.TrimSpace(record[seqIdx]))
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid shape_pt_sequence: %w", lineNum, err)
		}

		idKey := strings.TrimSpace(record[idIdx])
		shapeID, ok := shapeIntern[idKey]
		if !ok {
			shapeID = ShapeID(strings.Clone(idKey))
			shapeIntern[string(shapeID)] = shapeID
		}
		shapes[shapeID] = append(shapes[shapeID], ShapePoint{ShapeID: shapeID, Lat: lat, Lon: lon, Sequence: seq})
		loaded++
		sinceReport++
		if sinceReport >= chunkSize {
			sinceReport = 0
			if progress != nil {
				progress(loaded)
			}
		}
	}
	if progress != nil && (sinceReport > 0 || loaded == 0) {
		progress(loaded)
	}

	for shapeID, points := range shapes {
		sort.Slice(points, func(i, j int) bool { return points[i].Sequence < points[j].Sequence })
		shapes[shapeID] = points
	}
	return shapes, nil
}
