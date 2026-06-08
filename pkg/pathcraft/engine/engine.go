package engine

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/internal/geojson"
	"github.com/danielscoffee/pathcraft/internal/graph"
	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/internal/osm"
	"github.com/danielscoffee/pathcraft/internal/routing/astar"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
	pcTime "github.com/danielscoffee/pathcraft/internal/time"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
)

const defaultStopCandidates = 3

type Engine struct {
	graph         *graph.Graph
	gtfsIndex     *gtfs.StopTimeIndex
	gtfsStops     map[gtfs.StopID]gtfs.Stop
	gtfsTransfers map[gtfs.StopID][]raptor.Transfer
}

func New() *Engine {
	return &Engine{}
}

type RouteRequest struct {
	From               int64
	To                 int64
	Profile            mobility.Profile
	IncludeCoordinates bool
}

type Coordinate struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type RouteResult struct {
	Nodes       []int64       `json:"nodes"`
	Coordinates []Coordinate  `json:"coordinates,omitempty"`
	Distance    float64       `json:"distance_meters"`
	Duration    time.Duration `json:"duration"`
}

type CoordinateRouteRequest struct {
	FromLat             float64
	FromLon             float64
	ToLat               float64
	ToLon               float64
	Profile             mobility.Profile
	IncludeCoordinates  bool
	IncludeInputInShape bool
}

type CoordinateRouteResult struct {
	RouteResult
	FromNodeID        int64   `json:"from_node_id"`
	ToNodeID          int64   `json:"to_node_id"`
	FromSnapDistanceM float64 `json:"from_snap_distance_meters"`
	ToSnapDistanceM   float64 `json:"to_snap_distance_meters"`
}

type TransitRouteRequest struct {
	FromStop      string
	ToStop        string
	DepartureTime string // HH:MM:SS
}

type MultimodalRouteRequest struct {
	FromLat        float64
	FromLon        float64
	ToLat          float64
	ToLon          float64
	DepartureTime  string
	WalkingProfile mobility.Profile
	MaxStopCount   int
}

type JourneyLeg struct {
	Mode        string        `json:"mode"`
	FromName    string        `json:"from_name,omitempty"`
	ToName      string        `json:"to_name,omitempty"`
	FromStopID  string        `json:"from_stop_id,omitempty"`
	ToStopID    string        `json:"to_stop_id,omitempty"`
	TripID      string        `json:"trip_id,omitempty"`
	DistanceM   float64       `json:"distance_meters,omitempty"`
	Duration    time.Duration `json:"duration"`
	Nodes       []int64       `json:"nodes,omitempty"`
	Coordinates []Coordinate  `json:"coordinates,omitempty"`
}

type MultimodalRouteResult struct {
	Mode              string        `json:"mode"`
	DepartureTime     string        `json:"departure_time"`
	ArrivalTime       string        `json:"arrival_time"`
	TotalDuration     time.Duration `json:"total_duration"`
	TransitDuration   time.Duration `json:"transit_duration,omitempty"`
	WalkingDistanceM  float64       `json:"walking_distance_meters,omitempty"`
	TransitPath       []JourneyLeg  `json:"transit_path,omitempty"`
	Legs              []JourneyLeg  `json:"legs"`
	OriginStopID      string        `json:"origin_stop_id,omitempty"`
	DestinationStopID string        `json:"destination_stop_id,omitempty"`
}

type GraphStats struct {
	Nodes int
	Edges int
}

type GTFSStop struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

type GTFSTripStopTime struct {
	TripID        string  `json:"trip_id"`
	RouteID       string  `json:"route_id"`
	StopID        string  `json:"stop_id"`
	StopName      string  `json:"stop_name"`
	ArrivalTime   string  `json:"arrival_time"`
	DepartureTime string  `json:"departure_time"`
	StopSequence  int     `json:"stop_sequence"`
	Lat           float64 `json:"lat"`
	Lon           float64 `json:"lon"`
}

type stopCandidate struct {
	stop      gtfs.Stop
	distanceM float64
	nodeID    int64
	snapDistM float64
}

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
	e.gtfsTransfers = nil
	e.gtfsStops = nil
	return nil
}

func (e *Engine) LoadGTFSDir(dir string) error {
	stopTimesPath := filepath.Join(dir, "stop_times.txt")
	tripsPath := filepath.Join(dir, "trips.txt")
	transfersPath := filepath.Join(dir, "transfers.txt")
	stopsPath := filepath.Join(dir, "stops.txt")

	stopTimes, err := gtfs.ParseStopTimesFile(stopTimesPath)
	if err != nil {
		return fmt.Errorf("parsing stop_times: %w", err)
	}

	tripRoutes, err := gtfs.ParseTripsFile(tripsPath)
	if err != nil {
		return fmt.Errorf("parsing trips: %w", err)
	}

	e.gtfsIndex = gtfs.BuildIndex(stopTimes, tripRoutes)
	e.gtfsTransfers = make(map[gtfs.StopID][]raptor.Transfer)
	if transfers, err := gtfs.ParseTransfersFile(transfersPath); err == nil {
		for _, t := range transfers {
			e.gtfsTransfers[t.FromStopID] = append(e.gtfsTransfers[t.FromStopID], raptor.Transfer{
				To:       t.ToStopID,
				Duration: pcTime.Time(t.MinTransferTime),
			})
		}
	}

	if stops, err := gtfs.ParseStopsFile(stopsPath); err == nil {
		e.gtfsStops = stops
	}

	return nil
}

func (e *Engine) TransitRoute(req TransitRouteRequest) (*raptor.Result, error) {
	if e.gtfsIndex == nil {
		return nil, fmt.Errorf("GTFS not loaded")
	}
	if req.FromStop == "" || req.ToStop == "" {
		return nil, fmt.Errorf("from and to stops are required")
	}

	depTime, err := pcTime.ParseTime(req.DepartureTime)
	if err != nil {
		return nil, fmt.Errorf("invalid departure time: %w", err)
	}

	router := raptor.NewRouter(e.gtfsIndex, e.gtfsTransfers)
	res := router.Search(gtfs.StopID(req.FromStop), depTime)

	targetStop := gtfs.StopID(req.ToStop)
	if _, ok := res.EarliestArrival[targetStop]; !ok {
		return nil, fmt.Errorf("stop %s is not reachable from %s", req.ToStop, req.FromStop)
	}

	return res, nil
}

func (e *Engine) Route(req RouteRequest) (*RouteResult, error) {
	if e.graph == nil {
		return nil, fmt.Errorf("graph not loaded")
	}

	sourceID := graph.NodeID(req.From)
	targetID := graph.NodeID(req.To)

	if !e.graph.HasNode(sourceID) {
		return nil, fmt.Errorf("source node %d not found", req.From)
	}
	if !e.graph.HasNode(targetID) {
		return nil, fmt.Errorf("target node %d not found", req.To)
	}

	if req.Profile == nil {
		return nil, fmt.Errorf("routing profile is required")
	}

	return e.routeBetweenNodes(sourceID, targetID, req.Profile, req.IncludeCoordinates)
}

func (e *Engine) RouteByCoordinates(req CoordinateRouteRequest) (*CoordinateRouteResult, error) {
	if e.graph == nil {
		return nil, fmt.Errorf("graph not loaded")
	}
	if req.Profile == nil {
		return nil, fmt.Errorf("routing profile is required")
	}

	fromNodeID, fromSnapDist, err := e.NearestNode(req.FromLat, req.FromLon)
	if err != nil {
		return nil, err
	}

	toNodeID, toSnapDist, err := e.NearestNode(req.ToLat, req.ToLon)
	if err != nil {
		return nil, err
	}

	route, err := e.routeBetweenNodes(graph.NodeID(fromNodeID), graph.NodeID(toNodeID), req.Profile, req.IncludeCoordinates)
	if err != nil {
		return nil, err
	}

	if req.IncludeCoordinates && req.IncludeInputInShape {
		route.Coordinates = append([]Coordinate{{Lat: req.FromLat, Lon: req.FromLon}}, route.Coordinates...)
		route.Coordinates = append(route.Coordinates, Coordinate{Lat: req.ToLat, Lon: req.ToLon})
	}

	return &CoordinateRouteResult{
		RouteResult:       *route,
		FromNodeID:        fromNodeID,
		ToNodeID:          toNodeID,
		FromSnapDistanceM: fromSnapDist,
		ToSnapDistanceM:   toSnapDist,
	}, nil
}

func (e *Engine) MultimodalRoute(req MultimodalRouteRequest) (*MultimodalRouteResult, error) {
	if e.graph == nil {
		return nil, fmt.Errorf("graph not loaded")
	}
	if e.gtfsIndex == nil {
		return nil, fmt.Errorf("GTFS not loaded")
	}
	if len(e.gtfsStops) == 0 {
		return nil, fmt.Errorf("GTFS stops with coordinates are required for multimodal routing")
	}
	if req.WalkingProfile == nil {
		req.WalkingProfile = mobility.NewWalking(mobility.DefaultWalkingSpeedMPS)
	}
	if req.MaxStopCount <= 0 {
		req.MaxStopCount = defaultStopCandidates
	}

	departure, err := pcTime.ParseTime(req.DepartureTime)
	if err != nil {
		return nil, fmt.Errorf("invalid departure time: %w", err)
	}

	directWalk, err := e.RouteByCoordinates(CoordinateRouteRequest{
		FromLat:            req.FromLat,
		FromLon:            req.FromLon,
		ToLat:              req.ToLat,
		ToLon:              req.ToLon,
		Profile:            req.WalkingProfile,
		IncludeCoordinates: true,
	})
	if err != nil {
		return nil, err
	}

	best := &MultimodalRouteResult{
		Mode:             "walk",
		DepartureTime:    departure.String(),
		ArrivalTime:      addDuration(departure, directWalk.Duration).String(),
		TotalDuration:    directWalk.Duration,
		WalkingDistanceM: directWalk.Distance,
		Legs: []JourneyLeg{{
			Mode:        "walk",
			FromName:    "origin",
			ToName:      "destination",
			DistanceM:   directWalk.Distance,
			Duration:    directWalk.Duration,
			Nodes:       directWalk.Nodes,
			Coordinates: directWalk.Coordinates,
		}},
	}

	originNodeID := graph.NodeID(directWalk.FromNodeID)
	destinationNodeID := graph.NodeID(directWalk.ToNodeID)
	originCandidates := e.nearestStops(req.FromLat, req.FromLon, req.MaxStopCount)
	destinationCandidates := e.nearestStops(req.ToLat, req.ToLon, req.MaxStopCount)
	router := raptor.NewRouter(e.gtfsIndex, e.gtfsTransfers)

	for _, fromStop := range originCandidates {
		walkToTransit, err := e.routeBetweenNodes(originNodeID, graph.NodeID(fromStop.nodeID), req.WalkingProfile, true)
		if err != nil {
			continue
		}

		searchDeparture := addDuration(departure, walkToTransit.Duration)
		transitResult := router.Search(fromStop.stop.ID, searchDeparture)

		for _, toStop := range destinationCandidates {
			if toStop.stop.ID == fromStop.stop.ID {
				continue
			}

			transitArrival, ok := transitResult.EarliestArrival[toStop.stop.ID]
			if !ok {
				continue
			}

			walkFromTransit, err := e.routeBetweenNodes(graph.NodeID(toStop.nodeID), destinationNodeID, req.WalkingProfile, true)
			if err != nil {
				continue
			}

			totalDuration := durationBetween(departure, transitArrival) + walkFromTransit.Duration
			if totalDuration >= best.TotalDuration {
				continue
			}

			legs := []JourneyLeg{{
				Mode:        "walk",
				FromName:    "origin",
				ToName:      fromStop.stop.Name,
				FromStopID:  string(fromStop.stop.ID),
				DistanceM:   walkToTransit.Distance,
				Duration:    walkToTransit.Duration,
				Nodes:       walkToTransit.Nodes,
				Coordinates: walkToTransit.Coordinates,
			}}

			transitSteps := transitResult.ReconstructPath(toStop.stop.ID)
			if len(transitSteps) == 0 {
				continue
			}
			transitDuration := durationBetween(searchDeparture, transitArrival)
			for _, step := range transitSteps {
				leg := JourneyLeg{
					Mode:       "transit",
					FromName:   e.stopLabel(step.FromStop),
					ToName:     e.stopLabel(step.ToStop),
					FromStopID: string(step.FromStop),
					ToStopID:   string(step.ToStop),
					TripID:     string(step.TripID),
				}
				if step.IsTransfer {
					leg.Mode = "transfer"
				}
				legs = append(legs, leg)
			}

			legs = append(legs, JourneyLeg{
				Mode:        "walk",
				FromName:    toStop.stop.Name,
				ToName:      "destination",
				ToStopID:    string(toStop.stop.ID),
				DistanceM:   walkFromTransit.Distance,
				Duration:    walkFromTransit.Duration,
				Nodes:       walkFromTransit.Nodes,
				Coordinates: walkFromTransit.Coordinates,
			})

			best = &MultimodalRouteResult{
				Mode:              "multimodal",
				DepartureTime:     departure.String(),
				ArrivalTime:       addDuration(transitArrival, walkFromTransit.Duration).String(),
				TotalDuration:     totalDuration,
				TransitDuration:   transitDuration,
				WalkingDistanceM:  walkToTransit.Distance + walkFromTransit.Distance,
				TransitPath:       legs[1 : len(legs)-1],
				Legs:              legs,
				OriginStopID:      string(fromStop.stop.ID),
				DestinationStopID: string(toStop.stop.ID),
			}
		}
	}

	return best, nil
}

func (e *Engine) RouteGeoJSON(req RouteRequest) ([]byte, error) {
	res, err := e.Route(req)
	if err != nil {
		return nil, err
	}

	ids := make([]graph.NodeID, len(res.Nodes))
	for i, nodeID := range res.Nodes {
		ids[i] = graph.NodeID(nodeID)
	}

	return geojson.PathToGeoJSON(e.graph, ids), nil
}

func (e *Engine) RouteGeoJSONByCoordinates(req CoordinateRouteRequest) ([]byte, error) {
	res, err := e.RouteByCoordinates(req)
	if err != nil {
		return nil, err
	}

	ids := make([]graph.NodeID, len(res.Nodes))
	for i, nodeID := range res.Nodes {
		ids[i] = graph.NodeID(nodeID)
	}

	return geojson.PathToGeoJSON(e.graph, ids), nil
}

func (e *Engine) Stats() GraphStats {
	if e.graph == nil {
		return GraphStats{}
	}

	edgeCount := 0
	for _, edges := range e.graph.Edges {
		edgeCount += len(edges)
	}

	return GraphStats{Nodes: len(e.graph.Nodes), Edges: edgeCount}
}

func (e *Engine) NearestNode(lat, lon float64) (int64, float64, error) {
	if e.graph == nil {
		return 0, 0, fmt.Errorf("graph not loaded")
	}

	id, dist := e.graph.NearestNode(lat, lon, geo.HaversineDistance)
	return int64(id), dist, nil
}

// GetGraph returns the underlying graph.
// Note: This exposes internal implementation details and should be used with caution.
// It is primarily intended for the HTTP server adapter.
func (e *Engine) GetGraph() *graph.Graph {
	return e.graph
}

func (e *Engine) SetNearestNodeIndex(index plugins.NearestNodeIndex) error {
	if e.graph == nil {
		return fmt.Errorf("graph not loaded")
	}

	e.graph.SetNearestNodeIndex(index)
	return nil
}

func (e *Engine) GTFSStops() []GTFSStop {
	if len(e.gtfsStops) == 0 {
		return nil
	}

	stops := make([]GTFSStop, 0, len(e.gtfsStops))
	for _, stop := range e.gtfsStops {
		stops = append(stops, GTFSStop{
			ID:   string(stop.ID),
			Name: stop.Name,
			Lat:  stop.Lat,
			Lon:  stop.Lon,
		})
	}

	sort.Slice(stops, func(i, j int) bool {
		if stops[i].Name == stops[j].Name {
			return stops[i].ID < stops[j].ID
		}
		return stops[i].Name < stops[j].Name
	})

	return stops
}

func (e *Engine) GTFSTripIDs() []string {
	if e.gtfsIndex == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var tripIDs []string
	for _, routeTrips := range e.gtfsIndex.RouteTrips {
		for _, trip := range routeTrips {
			if len(trip) == 0 {
				continue
			}
			tripID := string(trip[0].TripID)
			if _, ok := seen[tripID]; ok {
				continue
			}
			seen[tripID] = struct{}{}
			tripIDs = append(tripIDs, tripID)
		}
	}

	sort.Strings(tripIDs)
	return tripIDs
}

func (e *Engine) GTFSTripStopTimes(tripID string) ([]GTFSTripStopTime, error) {
	if e.gtfsIndex == nil {
		return nil, fmt.Errorf("GTFS not loaded")
	}
	if len(e.gtfsStops) == 0 {
		return nil, fmt.Errorf("GTFS stops with coordinates are required")
	}
	if tripID == "" {
		return nil, fmt.Errorf("trip_id is required")
	}

	targetTripID := gtfs.TripID(tripID)
	for routeID, routeTrips := range e.gtfsIndex.RouteTrips {
		pattern := e.gtfsIndex.RoutePatterns[routeID]
		if pattern == nil {
			continue
		}

		for _, trip := range routeTrips {
			if len(trip) == 0 || trip[0].TripID != targetTripID {
				continue
			}

			stopTimes := make([]GTFSTripStopTime, 0, len(trip))
			for i, stopTime := range trip {
				if i >= len(pattern.Stops) {
					break
				}
				stopID := pattern.Stops[i].StopID
				stop := e.gtfsStops[stopID]
				stopTimes = append(stopTimes, GTFSTripStopTime{
					TripID:        string(stopTime.TripID),
					RouteID:       string(routeID),
					StopID:        string(stopID),
					StopName:      stop.Name,
					ArrivalTime:   stopTime.ArrivalTime.String(),
					DepartureTime: stopTime.DepartureTime.String(),
					StopSequence:  pattern.Stops[i].Sequence,
					Lat:           stop.Lat,
					Lon:           stop.Lon,
				})
			}

			return stopTimes, nil
		}
	}

	return nil, fmt.Errorf("trip %s not found", tripID)
}

func (e *Engine) routeBetweenNodes(sourceID, targetID graph.NodeID, profile mobility.Profile, includeCoordinates bool) (*RouteResult, error) {
	speed := profile.Speed()
	if speed <= 0 {
		speed = mobility.DefaultWalkingSpeedMPS
	}

	heuristic := geo.HaversineHeuristic(speed)
	path, err := astar.AStar(e.graph, sourceID, targetID, heuristic)
	if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}

	nodes := make([]int64, len(path.Nodes))
	var coords []Coordinate
	if includeCoordinates {
		coords = make([]Coordinate, len(path.Nodes))
	}

	for i, n := range path.Nodes {
		nodes[i] = int64(n)
		if includeCoordinates {
			node := e.graph.Nodes[n]
			coords[i] = Coordinate{Lat: node.Lat, Lon: node.Lon}
		}
	}

	durationSeconds := path.TotalCost / speed
	duration := time.Duration(durationSeconds * float64(time.Second))

	return &RouteResult{Nodes: nodes, Coordinates: coords, Distance: path.TotalCost, Duration: duration}, nil
}

func (e *Engine) nearestStops(lat, lon float64, limit int) []stopCandidate {
	if limit <= 0 {
		return nil
	}

	candidates := make([]stopCandidate, 0, len(e.gtfsStops))
	for _, stop := range e.gtfsStops {
		nodeID, snapDist, err := e.NearestNode(stop.Lat, stop.Lon)
		if err != nil {
			continue
		}
		candidates = append(candidates, stopCandidate{
			stop:      stop,
			distanceM: geo.HaversineDistance(lat, lon, stop.Lat, stop.Lon),
			nodeID:    nodeID,
			snapDistM: snapDist,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].distanceM < candidates[j].distanceM
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	return candidates
}

func (e *Engine) stopLabel(id gtfs.StopID) string {
	if stop, ok := e.gtfsStops[id]; ok && stop.Name != "" {
		return stop.Name
	}
	return string(id)
}

func addDuration(t pcTime.Time, d time.Duration) pcTime.Time {
	return t + pcTime.Time(d/time.Second)
}

func durationBetween(from, to pcTime.Time) time.Duration {
	return time.Duration(int(to-from)) * time.Second
}
