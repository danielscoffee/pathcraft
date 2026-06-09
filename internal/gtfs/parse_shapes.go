package gtfs

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

func ParseShapes(r io.Reader) (map[ShapeID][]ShapePoint, error) {
	csvReader := csv.NewReader(r)
	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}
	requiredCols := []string{"shape_id", "shape_pt_lat", "shape_pt_lon", "shape_pt_sequence"}
	for _, col := range requiredCols {
		if _, ok := colIndex[col]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingColumn, col)
		}
	}
	idIdx := colIndex["shape_id"]
	latIdx := colIndex["shape_pt_lat"]
	lonIdx := colIndex["shape_pt_lon"]
	seqIdx := colIndex["shape_pt_sequence"]

	shapes := make(map[ShapeID][]ShapePoint)
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
		shapeID := ShapeID(strings.TrimSpace(record[idIdx]))
		shapes[shapeID] = append(shapes[shapeID], ShapePoint{ShapeID: shapeID, Lat: lat, Lon: lon, Sequence: seq})
	}
	for shapeID, points := range shapes {
		sort.Slice(points, func(i, j int) bool { return points[i].Sequence < points[j].Sequence })
		shapes[shapeID] = points
	}
	return shapes, nil
}

func ParseShapesFile(path string) (map[ShapeID][]ShapePoint, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseShapes(f)
}
