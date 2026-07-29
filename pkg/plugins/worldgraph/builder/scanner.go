package builder

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
	paul "github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
)

const (
	DefaultMaxWayNodes = 100_000
	nodeBatchSize      = 4_096
	maxScannerWorkers  = 4
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
	if err := validatePBF(ctx, path, DefaultMaxWayNodes); err != nil {
		return err
	}
	return scanNodesUnchecked(ctx, path, consume)
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

	scanner := osmpbf.New(ctx, file, scannerWorkers())
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
	if err := validatePBF(ctx, path, maxWayNodes); err != nil {
		return err
	}
	return scanWaysUnchecked(ctx, path, maxWayNodes, consume)
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

	scanner := osmpbf.New(ctx, file, scannerWorkers())
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
