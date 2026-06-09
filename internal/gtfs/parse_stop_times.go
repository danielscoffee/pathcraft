package gtfs

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/danielscoffee/pathcraft/internal/time"
)

func ParseStopTimes(r io.Reader) ([]StopTime, error) {
	csvReader := csv.NewReader(r)

	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}

	requiredCols := []string{"trip_id", "stop_id", "arrival_time", "departure_time", "stop_sequence"}
	for _, col := range requiredCols {
		if _, ok := colIndex[col]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingColumn, col)
		}
	}

	tripIdx := colIndex["trip_id"]
	stopIdx := colIndex["stop_id"]
	arrivalIdx := colIndex["arrival_time"]
	departureIdx := colIndex["departure_time"]
	seqIdx := colIndex["stop_sequence"]

	var stopTimes []StopTime

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

		arrivalTime, err := time.ParseTime(record[arrivalIdx])
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid arrival_time: %w", lineNum, err)
		}

		departureTime, err := time.ParseTime(record[departureIdx])
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid departure_time: %w", lineNum, err)
		}

		stopSequence, err := strconv.Atoi(strings.TrimSpace(record[seqIdx]))
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid stop_sequence: %w", lineNum, err)
		}

		stopTimes = append(stopTimes, StopTime{
			TripID:        TripID(strings.TrimSpace(record[tripIdx])),
			StopID:        StopID(strings.TrimSpace(record[stopIdx])),
			ArrivalTime:   arrivalTime,
			DepartureTime: departureTime,
			StopSequence:  stopSequence,
		})
	}

	return stopTimes, nil
}

func ParseStopTimesFile(path string) ([]StopTime, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseStopTimes(f)
}
