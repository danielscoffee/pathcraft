package engine

import (
	"fmt"
	"path/filepath"

	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/logging"
	"github.com/danielscoffee/pathcraft/internal/osm"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
	"go.uber.org/zap"
)

func (e *Engine) LoadOSM(path string) error {
	data, err := osm.ParseFile(path)
	if err != nil {
		return fmt.Errorf("parsing OSM file: %w", err)
	}
	fingerprint, err := graph.FingerprintFile(path)
	if err != nil {
		return fmt.Errorf("fingerprinting OSM file: %w", err)
	}

	g := osm.BuildGraph(data, nil)
	g.Contraction, _ = graph.BuildDegreeTwoContraction(g)
	e.graph = g
	e.graphSourceSHA256 = fingerprint
	return nil
}

func (e *Engine) SaveGraph(path string) error {
	if e.graph == nil {
		return fmt.Errorf("graph not loaded")
	}
	return e.graph.SaveCache(path, graph.CacheMetadata{SourceSHA256: e.graphSourceSHA256})
}

func (e *Engine) LoadGraph(path string) error {
	g, metadata, err := graph.LoadGraphCache(path)
	if err != nil {
		return err
	}
	e.graph = g
	e.graphSourceSHA256 = metadata.SourceSHA256
	return nil
}

func (e *Engine) LoadGTFS(stopTimesPath, tripsPath string) error {
	stopTimes, err := gtfs.ParseStopTimesFile(stopTimesPath)
	if err != nil {
		return fmt.Errorf("parsing stop_times: %w", err)
	}

	tripRoutes, err := gtfs.ParseTripsFile(tripsPath)
	if err != nil {
		return fmt.Errorf("parsing trips: %w", err)
	}

	e.gtfsIndex = gtfs.BuildIndex(stopTimes, tripRoutes)
	e.gtfsTripRoutes = tripRoutes
	e.gtfsTransfers = nil
	e.gtfsStops = nil
	e.gtfsRoutes = nil
	return nil
}

func (e *Engine) LoadGTFSDir(dir string) error {
	stopTimesPath := filepath.Join(dir, "stop_times.txt")
	tripsPath := filepath.Join(dir, "trips.txt")
	transfersPath := filepath.Join(dir, "transfers.txt")
	stopsPath := filepath.Join(dir, "stops.txt")
	shapesPath := filepath.Join(dir, "shapes.txt")

	// City-scale feeds are streamed in chunks: Grande Recife's stop_times
	// alone is 3.1M rows / 180 MB, too big to double-buffer as one slice.
	builder, totalRows, err := gtfs.LoadStopTimesChunked(stopTimesPath, gtfs.DefaultChunkSize, func(rows int) {
		logging.L().Info("loading GTFS stop_times", zap.Int("rows", rows))
	})
	if err != nil {
		return fmt.Errorf("parsing stop_times: %w", err)
	}
	logging.L().Info("GTFS stop_times loaded", zap.Int("rows", totalRows))

	tripRoutes, err := gtfs.ParseTripsFile(tripsPath)
	if err != nil {
		return fmt.Errorf("parsing trips: %w", err)
	}

	e.gtfsIndex = builder.Build(tripRoutes)
	e.gtfsTripRoutes = tripRoutes
	e.gtfsTripShapes = nil
	if infos, err := gtfs.ParseTripInfosFile(tripsPath); err == nil {
		e.gtfsTripShapes = make(map[gtfs.TripID]gtfs.ShapeID, len(infos))
		for tripID, info := range infos {
			if info.ShapeID != "" {
				e.gtfsTripShapes[tripID] = info.ShapeID
			}
		}
	}
	e.gtfsRoutes = nil
	if routes, err := gtfs.ParseRoutesFile(filepath.Join(dir, "routes.txt")); err == nil {
		e.gtfsRoutes = routes
	}
	e.gtfsShapes = nil
	if shapes, err := gtfs.LoadShapesChunked(shapesPath, gtfs.DefaultChunkSize, func(points int) {
		logging.L().Info("loading GTFS shapes", zap.Int("points", points))
	}); err == nil {
		e.gtfsShapes = shapes
	}
	if stops, err := gtfs.ParseStopsFile(stopsPath); err == nil {
		e.gtfsStops = stops
	}

	e.gtfsTransfers = make(map[gtfs.StopID][]raptor.Transfer)
	if transfers, err := gtfs.ParseTransfersFile(transfersPath); err == nil {
		for _, t := range transfers {
			e.gtfsTransfers[t.FromStopID] = append(e.gtfsTransfers[t.FromStopID], raptor.Transfer{
				To:       t.ToStopID,
				Duration: pcTime.Time(t.MinTransferTime),
			})
		}
	} else if len(e.gtfsStops) > 0 {
		e.gtfsTransfers = inferNearbyStopTransfers(e.gtfsStops, 80, 120)
	}

	return nil
}
