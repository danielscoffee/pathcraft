package engine

import (
	"fmt"
	"path/filepath"

	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/osm"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
)

func (e *Engine) LoadOSM(path string) error {
	data, err := osm.ParseFile(path)
	if err != nil {
		return fmt.Errorf("parsing OSM file: %w", err)
	}

	e.graph = osm.BuildGraph(data, nil)
	return nil
}

func (e *Engine) SaveGraph(path string) error {
	if e.graph == nil {
		return fmt.Errorf("graph not loaded")
	}
	return e.graph.Save(path)
}

func (e *Engine) LoadGraph(path string) error {
	g, err := graph.LoadGraph(path)
	if err != nil {
		return err
	}
	e.graph = g
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
	if shapes, err := gtfs.ParseShapesFile(shapesPath); err == nil {
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
