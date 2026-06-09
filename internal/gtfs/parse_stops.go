package gtfs

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type Stop struct {
	ID   StopID
	Name string
	Lat  float64
	Lon  float64
}

func ParseStops(r io.Reader) (map[StopID]Stop, error) {
	csvReader := csv.NewReader(r)

	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}

	requiredCols := []string{"stop_id", "stop_name", "stop_lat", "stop_lon"}
	for _, col := range requiredCols {
		if _, ok := colIndex[col]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingColumn, col)
		}
	}

	idxStopID := colIndex["stop_id"]
	idxStopName := colIndex["stop_name"]
	idxStopLat := colIndex["stop_lat"]
	idxStopLon := colIndex["stop_lon"]

	stops := make(map[StopID]Stop)
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

		lat, err := strconv.ParseFloat(strings.TrimSpace(record[idxStopLat]), 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid stop_lat: %w", lineNum, err)
		}

		lon, err := strconv.ParseFloat(strings.TrimSpace(record[idxStopLon]), 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid stop_lon: %w", lineNum, err)
		}

		stopID := StopID(strings.TrimSpace(record[idxStopID]))
		stops[stopID] = Stop{
			ID:   stopID,
			Name: strings.TrimSpace(record[idxStopName]),
			Lat:  lat,
			Lon:  lon,
		}
	}

	return stops, nil
}

func ParseStopsFile(path string) (map[StopID]Stop, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseStops(f)
}
