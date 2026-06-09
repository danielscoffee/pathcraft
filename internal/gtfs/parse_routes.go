package gtfs

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

func ParseRoutes(r io.Reader) (map[RouteID]Route, error) {
	csvReader := csv.NewReader(r)
	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}
	idIdx, ok := colIndex["route_id"]
	if !ok {
		return nil, fmt.Errorf("%w: route_id", ErrMissingColumn)
	}
	shortIdx, hasShort := colIndex["route_short_name"]
	longIdx, hasLong := colIndex["route_long_name"]

	routes := make(map[RouteID]Route)
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
		id := RouteID(strings.TrimSpace(record[idIdx]))
		route := Route{ID: id}
		if hasShort && shortIdx < len(record) {
			route.ShortName = strings.TrimSpace(record[shortIdx])
		}
		if hasLong && longIdx < len(record) {
			route.LongName = strings.TrimSpace(record[longIdx])
		}
		routes[id] = route
	}
	return routes, nil
}

func ParseRoutesFile(path string) (map[RouteID]Route, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseRoutes(f)
}
