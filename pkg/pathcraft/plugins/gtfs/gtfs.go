// Package gtfs registers the "gtfs" GraphLoader that ingests a directory
// containing stop_times.txt, trips.txt, and optionally transfers.txt and
// stops.txt. The returned core.Graph implements gtfsgraph.Native so the
// raptor algorithm plugin can recover the underlying StopTimeIndex.
package gtfs

import (
	"context"
	"fmt"
	"path/filepath"

	igtfs "github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/plugins/gtfsgraph"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/registry"
)

type Loader struct{}

func (Loader) Name() string { return "gtfs" }

func (Loader) Load(_ context.Context, source string) (core.Graph, error) {
	if source == "" {
		return nil, fmt.Errorf("gtfs loader: source directory is required")
	}

	stopTimes, err := igtfs.ParseStopTimesFile(filepath.Join(source, "stop_times.txt"))
	if err != nil {
		return nil, fmt.Errorf("gtfs loader: stop_times: %w", err)
	}
	trips, err := igtfs.ParseTripsFile(filepath.Join(source, "trips.txt"))
	if err != nil {
		return nil, fmt.Errorf("gtfs loader: trips: %w", err)
	}
	index := igtfs.BuildIndex(stopTimes, trips)

	transfers := make(map[igtfs.StopID][]raptor.Transfer)
	if tx, err := igtfs.ParseTransfersFile(filepath.Join(source, "transfers.txt")); err == nil {
		for _, t := range tx {
			transfers[t.FromStopID] = append(transfers[t.FromStopID], raptor.Transfer{
				To:       t.ToStopID,
				Duration: pcTime.Time(t.MinTransferTime),
			})
		}
	}

	stops, _ := igtfs.ParseStopsFile(filepath.Join(source, "stops.txt"))

	return &gtfsgraph.Wrapper{Index: index, Transfers: transfers, Stops: stops}, nil
}

func init() { registry.MustRegisterLoader(Loader{}) }
