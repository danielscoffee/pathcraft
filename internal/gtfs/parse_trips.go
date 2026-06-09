package gtfs

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

func ParseTrips(r io.Reader) (TripToRoute, error) {
	csvReader := csv.NewReader(r)

	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}

	requiredCols := []string{"trip_id", "route_id"}
	for _, col := range requiredCols {
		if _, ok := colIndex[col]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingColumn, col)
		}
	}

	tripIdx := colIndex["trip_id"]
	routeIdx := colIndex["route_id"]

	tripRoutes := make(TripToRoute)

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

		tripID := TripID(strings.TrimSpace(record[tripIdx]))
		routeID := RouteID(strings.TrimSpace(record[routeIdx]))
		tripRoutes[tripID] = routeID
	}

	return tripRoutes, nil
}

func ParseTripsFile(path string) (TripToRoute, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseTrips(f)
}

func ParseTripInfos(r io.Reader) (map[TripID]TripInfo, error) {
	csvReader := csv.NewReader(r)
	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}
	tripIdx, hasTrip := colIndex["trip_id"]
	routeIdx, hasRoute := colIndex["route_id"]
	if !hasTrip {
		return nil, fmt.Errorf("%w: trip_id", ErrMissingColumn)
	}
	if !hasRoute {
		return nil, fmt.Errorf("%w: route_id", ErrMissingColumn)
	}
	shapeIdx, hasShape := colIndex["shape_id"]

	infos := make(map[TripID]TripInfo)
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
		tripID := TripID(strings.TrimSpace(record[tripIdx]))
		info := TripInfo{TripID: tripID, RouteID: RouteID(strings.TrimSpace(record[routeIdx]))}
		if hasShape && shapeIdx < len(record) {
			info.ShapeID = ShapeID(strings.TrimSpace(record[shapeIdx]))
		}
		infos[tripID] = info
	}
	return infos, nil
}

func ParseTripInfosFile(path string) (map[TripID]TripInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseTripInfos(f)
}
