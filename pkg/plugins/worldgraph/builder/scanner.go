package builder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
	paul "github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
)

const (
	DefaultMaxWayNodes = 100_000
	nodeBatchSize      = 4_096

	// Filters run inside decoder workers. One worker preserves source order while
	// returning false from each filter keeps decoded object blocks out of the
	// dependency's buffered output queues.
	streamingScannerWorkers = 1
)

var (
	ErrWayNodeLimit   = errors.New("OSM way node limit exceeded")
	errScannerStopped = errors.New("OSM scanner stopped by consumer")
)

type boundedScannerReader struct {
	ctx     context.Context
	reader  io.Reader
	stopped atomic.Bool
}

func (reader *boundedScannerReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	if reader.stopped.Load() {
		return 0, errScannerStopped
	}
	return reader.reader.Read(buffer)
}

type Way struct {
	ID      int64
	NodeIDs []int64
	Tags    map[string]string
}

func ScanNodes(ctx context.Context, path string, consume func([]worldgraph.Node) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("node consumer is nil")
	}
	return scanValidatedPBFSnapshot(ctx, path, DefaultMaxWayNodes, func(snapshot string) error {
		return scanNodesUnchecked(ctx, snapshot, consume)
	})
}

func scanNodesUnchecked(ctx context.Context, path string, consume func([]worldgraph.Node) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return scanNodesReader(ctx, file, consume)
}

func scanNodesReader(ctx context.Context, reader io.Reader, consume func([]worldgraph.Node) error) error {
	return scanNodesReaderWithCopy(ctx, reader, consume, copyNode)
}

func scanGlobalNodesReader(ctx context.Context, reader io.Reader, consume func([]worldgraph.Node) error) error {
	return scanNodesReaderWithCopy(ctx, reader, consume, copyGlobalNode)
}

func scanNodesReaderWithCopy(
	ctx context.Context,
	reader io.Reader,
	consume func([]worldgraph.Node) error,
	copy func(*paul.Node) (worldgraph.Node, error),
) error {
	if ctx == nil {
		return fmt.Errorf("node scan context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if reader == nil || consume == nil || copy == nil {
		return fmt.Errorf("node reader, consumer, and copier are required")
	}

	type nodeBatchResult struct {
		nodes []worldgraph.Node
		err   error
	}
	source := &boundedScannerReader{ctx: ctx, reader: reader}
	// The dependency mutates scanner error state concurrently when its context
	// is canceled. Stop through the reader instead, so decoding drains in order
	// without racing Scanner.Next.
	scanner := osmpbf.New(context.Background(), source, streamingScannerWorkers)
	scanner.SkipWays = true
	scanner.SkipRelations = true
	results := make(chan nodeBatchResult, 1)
	scanDone := make(chan error, 1)
	filterBatch := make([]worldgraph.Node, 0, nodeBatchSize)
	scanner.FilterNode = func(node *paul.Node) bool {
		if source.stopped.Load() || ctx.Err() != nil {
			return false
		}
		copied, err := copy(node)
		if err != nil {
			filterBatch = nil
			results <- nodeBatchResult{err: err}
			source.stopped.Store(true)
			return false
		}
		filterBatch = append(filterBatch, copied)
		if len(filterBatch) == nodeBatchSize {
			results <- nodeBatchResult{nodes: filterBatch}
			filterBatch = make([]worldgraph.Node, 0, nodeBatchSize)
		}
		// Keeping decoded nodes out of Scanner.Object prevents whole primitive
		// blocks from accumulating in the dependency's buffered output channels.
		return false
	}
	go func() {
		for scanner.Scan() {
		}
		scanErr := scanner.Err()
		if scanErr == nil && len(filterBatch) > 0 {
			results <- nodeBatchResult{nodes: filterBatch}
		}
		scanDone <- scanErr
		close(results)
	}()

	var callbackErr error
	for result := range results {
		if callbackErr != nil || ctx.Err() != nil {
			source.stopped.Store(true)
			continue
		}
		if result.err != nil {
			callbackErr = result.err
			source.stopped.Store(true)
			continue
		}
		if err := consume(result.nodes); err != nil {
			callbackErr = err
			source.stopped.Store(true)
		}
	}
	scanErr := <-scanDone
	closeErr := scanner.Close()
	if callbackErr != nil {
		return errors.Join(callbackErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, closeErr)
	}
	if scanErr != nil {
		return errors.Join(scanErr, closeErr)
	}
	return closeErr
}

func ScanWays(ctx context.Context, path string, consume func(Way) error) error {
	return scanWays(ctx, path, DefaultMaxWayNodes, consume)
}

func scanWays(ctx context.Context, path string, maxWayNodes int, consume func(Way) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if maxWayNodes <= 0 {
		return fmt.Errorf("maximum way node count must be positive")
	}
	if consume == nil {
		return fmt.Errorf("way consumer is nil")
	}
	return scanValidatedPBFSnapshot(ctx, path, maxWayNodes, func(snapshot string) error {
		return scanWaysUnchecked(ctx, snapshot, maxWayNodes, consume)
	})
}

func scanValidatedPBFSnapshot(ctx context.Context, path string, maxWayNodes int, scan func(string) error) error {
	workDir, err := os.MkdirTemp("", "pathcraft-worldgraph-scan-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	snapshot := filepath.Join(workDir, "input.osm.pbf")
	if _, err := snapshotPBF(ctx, path, snapshot); err != nil {
		return err
	}
	if err := validatePBF(ctx, snapshot, maxWayNodes); err != nil {
		return err
	}
	return scan(snapshot)
}

func scanWaysUnchecked(ctx context.Context, path string, maxWayNodes int, consume func(Way) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return scanWaysReader(ctx, file, maxWayNodes, consume)
}

func scanWaysReader(ctx context.Context, reader io.Reader, maxWayNodes int, consume func(Way) error) error {
	if ctx == nil {
		return fmt.Errorf("way scan context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if reader == nil || consume == nil {
		return fmt.Errorf("way reader and consumer are required")
	}
	if maxWayNodes < 1 {
		return fmt.Errorf("maximum way node count must be positive")
	}

	type wayResult struct {
		way Way
		err error
	}
	source := &boundedScannerReader{ctx: ctx, reader: reader}
	scanner := osmpbf.New(context.Background(), source, streamingScannerWorkers)
	scanner.SkipNodes = true
	scanner.SkipRelations = true
	results := make(chan wayResult, 1)
	scanDone := make(chan error, 1)
	scanner.FilterWay = func(way *paul.Way) bool {
		if source.stopped.Load() || ctx.Err() != nil {
			return false
		}
		copied, err := copyWay(way, maxWayNodes)
		results <- wayResult{way: copied, err: err}
		if err != nil {
			source.stopped.Store(true)
		}
		return false
	}
	go func() {
		for scanner.Scan() {
		}
		scanDone <- scanner.Err()
		close(results)
	}()

	var callbackErr error
	for result := range results {
		if callbackErr != nil || ctx.Err() != nil {
			source.stopped.Store(true)
			continue
		}
		if result.err != nil {
			callbackErr = result.err
			source.stopped.Store(true)
			continue
		}
		if err := consume(result.way); err != nil {
			callbackErr = err
			source.stopped.Store(true)
		}
	}
	scanErr := <-scanDone
	closeErr := scanner.Close()
	if callbackErr != nil {
		return errors.Join(callbackErr, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, closeErr)
	}
	if scanErr != nil {
		return errors.Join(scanErr, closeErr)
	}
	return closeErr
}

func copyNode(node *paul.Node) (worldgraph.Node, error) {
	if node == nil || !validMercatorPosition(node.Lon, node.Lat) {
		return worldgraph.Node{}, fmt.Errorf("invalid OSM node coordinates")
	}
	return worldgraph.Node{ID: int64(node.ID), Lon: node.Lon, Lat: node.Lat}, nil
}

func copyGlobalNode(node *paul.Node) (worldgraph.Node, error) {
	if node == nil || !validOSMPosition(node.Lon, node.Lat) {
		return worldgraph.Node{}, fmt.Errorf("invalid OSM node coordinates")
	}
	return worldgraph.Node{ID: int64(node.ID), Lon: node.Lon, Lat: node.Lat}, nil
}

func validOSMPosition(lon, lat float64) bool {
	return finiteCoordinate(lon) && lon >= -180 && lon <= 180 &&
		finiteCoordinate(lat) && lat >= -90 && lat <= 90
}

func validMercatorPosition(lon, lat float64) bool {
	return validOSMPosition(lon, lat) && lat >= -worldgraph.MaxMercatorLatitude && lat <= worldgraph.MaxMercatorLatitude
}

func copyWay(way *paul.Way, maxWayNodes int) (Way, error) {
	if way == nil {
		return Way{}, fmt.Errorf("nil OSM way")
	}
	if len(way.Nodes) > maxWayNodes {
		return Way{}, fmt.Errorf("%w: way %d has %d nodes, limit %d", ErrWayNodeLimit, way.ID, len(way.Nodes), maxWayNodes)
	}
	copied := Way{
		ID:      int64(way.ID),
		NodeIDs: make([]int64, len(way.Nodes)),
		Tags:    make(map[string]string, len(way.Tags)),
	}
	for index, node := range way.Nodes {
		copied.NodeIDs[index] = int64(node.ID)
	}
	for _, tag := range way.Tags {
		copied.Tags[tag.Key] = tag.Value
	}
	return copied, nil
}

func finiteCoordinate(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
