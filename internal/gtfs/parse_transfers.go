package gtfs

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type Transfer struct {
	FromStopID      StopID
	ToStopID        StopID
	TransferType    int
	MinTransferTime int // seconds
}

func ParseTransfers(r io.Reader) ([]Transfer, error) {
	csvReader := csv.NewReader(r)

	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}

	requiredCols := []string{"from_stop_id", "to_stop_id"}
	for _, col := range requiredCols {
		if _, ok := colIndex[col]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingColumn, col)
		}
	}

	fromIdx := colIndex["from_stop_id"]
	toIdx := colIndex["to_stop_id"]
	typeIdx, hasType := colIndex["transfer_type"]
	timeIdx, hasTime := colIndex["min_transfer_time"]

	var transfers []Transfer

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

		transfer := Transfer{
			FromStopID: StopID(strings.TrimSpace(record[fromIdx])),
			ToStopID:   StopID(strings.TrimSpace(record[toIdx])),
		}

		if hasType && typeIdx < len(record) {
			transfer.TransferType, _ = strconv.Atoi(strings.TrimSpace(record[typeIdx]))
		}

		if hasTime && timeIdx < len(record) {
			transfer.MinTransferTime, _ = strconv.Atoi(strings.TrimSpace(record[timeIdx]))
		}

		transfers = append(transfers, transfer)
	}

	return transfers, nil
}

func ParseTransfersFile(path string) ([]Transfer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseTransfers(f)
}
