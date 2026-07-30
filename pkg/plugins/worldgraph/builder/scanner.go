package builder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
	paul "github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
)

const (
	DefaultMaxWayNodes = 100_000
	nodeBatchSize      = 4_096
	maxScannerWorkers  = 2
)

var ErrWayNodeLimit = errors.New("OSM way node limit exceeded")

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
	if ctx == nil {
		return fmt.Errorf("node scan context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if reader == nil || consume == nil {
		return fmt.Errorf("node reader and consumer are required")
	}
	scanner := osmpbf.New(ctx, reader, scannerWorkers())
	scanner.SkipWays = true
	scanner.SkipRelations = true
	defer scanner.Close()

	batch := make([]worldgraph.Node, 0, nodeBatchSize)
	for scanner.Scan() {
		node, ok := scanner.Object().(*paul.Node)
		if !ok {
			continue
		}
		copied, err := copyNode(node)
		if err != nil {
			return err
		}
		batch = append(batch, copied)
		if len(batch) == nodeBatchSize {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := consume(batch); err != nil {
				return err
			}
			batch = make([]worldgraph.Node, 0, nodeBatchSize)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(batch) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return consume(batch)
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
	scanner := osmpbf.New(ctx, reader, scannerWorkers())
	scanner.SkipNodes = true
	scanner.SkipRelations = true
	defer scanner.Close()

	for scanner.Scan() {
		way, ok := scanner.Object().(*paul.Way)
		if !ok {
			continue
		}
		copied, err := copyWay(way, maxWayNodes)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := consume(copied); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func copyNode(node *paul.Node) (worldgraph.Node, error) {
	if node == nil || !finiteCoordinate(node.Lon) || node.Lon < -180 || node.Lon > 180 ||
		!finiteCoordinate(node.Lat) || node.Lat < -worldgraph.MaxMercatorLatitude || node.Lat > worldgraph.MaxMercatorLatitude {
		return worldgraph.Node{}, fmt.Errorf("invalid OSM node coordinates")
	}
	return worldgraph.Node{ID: int64(node.ID), Lon: node.Lon, Lat: node.Lat}, nil
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

func scannerWorkers() int {
	return min(max(runtime.GOMAXPROCS(0), 1), maxScannerWorkers)
}

func finiteCoordinate(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
