package builder

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

const globalNodeRecordBytes = 40

type globalNodeRecord struct {
	ID    int64
	Lon   float64
	Lat   float64
	Owner worldgraph.TileID
}

type globalNodeIndex struct {
	mu    sync.RWMutex
	file  *os.File
	count int64
}

func openGlobalNodeIndex(path string) (*globalNodeIndex, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() {
		return nil, errors.Join(fmt.Errorf("global node index is not a regular file"), file.Close())
	}
	if info.Size()%globalNodeRecordBytes != 0 {
		return nil, errors.Join(fmt.Errorf("%w: global node index size %d", ErrTruncatedFixedRecord, info.Size()), file.Close())
	}
	index := &globalNodeIndex{file: file, count: info.Size() / globalNodeRecordBytes}
	var previous int64
	for position := int64(0); position < index.count; position++ {
		record, err := index.readRecord(position)
		if err != nil {
			return nil, errors.Join(err, file.Close())
		}
		if position > 0 && record.ID <= previous {
			return nil, errors.Join(fmt.Errorf("global node index IDs are not strictly sorted"), file.Close())
		}
		previous = record.ID
	}
	return index, nil
}

func (index *globalNodeIndex) Get(id int64) (worldgraph.Node, bool, error) {
	if index == nil {
		return worldgraph.Node{}, false, fmt.Errorf("global node index is nil")
	}
	index.mu.RLock()
	defer index.mu.RUnlock()
	if index.file == nil {
		return worldgraph.Node{}, false, fmt.Errorf("global node index is closed")
	}
	low, high := int64(0), index.count
	for low < high {
		middle := low + (high-low)/2
		record, err := index.readRecord(middle)
		if err != nil {
			return worldgraph.Node{}, false, err
		}
		if record.ID < id {
			low = middle + 1
		} else {
			high = middle
		}
	}
	if low >= index.count {
		return worldgraph.Node{}, false, nil
	}
	record, err := index.readRecord(low)
	if err != nil {
		return worldgraph.Node{}, false, err
	}
	if record.ID != id {
		return worldgraph.Node{}, false, nil
	}
	return worldgraph.Node{ID: record.ID, Lon: record.Lon, Lat: record.Lat, Owner: record.Owner}, true, nil
}

func (index *globalNodeIndex) Close() error {
	if index == nil {
		return nil
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	if index.file == nil {
		return nil
	}
	err := index.file.Close()
	index.file = nil
	return err
}

func (index *globalNodeIndex) readRecord(position int64) (globalNodeRecord, error) {
	var encoded [globalNodeRecordBytes]byte
	count, err := index.file.ReadAt(encoded[:], position*globalNodeRecordBytes)
	if err != nil && !(errors.Is(err, io.EOF) && count == len(encoded)) {
		return globalNodeRecord{}, err
	}
	if count != len(encoded) {
		return globalNodeRecord{}, fmt.Errorf("%w: global node record", ErrTruncatedFixedRecord)
	}
	return decodeGlobalNodeRecord(encoded[:])
}

func writeGlobalNodeRecord(writer io.Writer, record globalNodeRecord) error {
	encoded, err := encodeGlobalNodeRecord(record)
	if err != nil {
		return err
	}
	return writeFixedRecord(writer, encoded[:])
}

func encodeGlobalNodeRecord(record globalNodeRecord) ([globalNodeRecordBytes]byte, error) {
	var encoded [globalNodeRecordBytes]byte
	if err := validateGlobalNodeRecord(record); err != nil {
		return encoded, err
	}
	copy(encoded[0:8], nodeKey(record.ID))
	binary.BigEndian.PutUint64(encoded[8:16], math.Float64bits(record.Lon))
	binary.BigEndian.PutUint64(encoded[16:24], math.Float64bits(record.Lat))
	binary.BigEndian.PutUint32(encoded[24:28], uint32(record.Owner.Z))
	binary.BigEndian.PutUint32(encoded[28:32], uint32(record.Owner.X))
	binary.BigEndian.PutUint32(encoded[32:36], uint32(record.Owner.Y))
	return encoded, nil
}

func decodeGlobalNodeRecord(encoded []byte) (globalNodeRecord, error) {
	if len(encoded) != globalNodeRecordBytes {
		return globalNodeRecord{}, fmt.Errorf("%w: global node record size %d", ErrTruncatedFixedRecord, len(encoded))
	}
	if binary.BigEndian.Uint32(encoded[36:40]) != 0 {
		return globalNodeRecord{}, fmt.Errorf("%w: global node record reserved bytes", worldgraph.ErrInvalidChunk)
	}
	ownerZ := binary.BigEndian.Uint32(encoded[24:28])
	ownerX := binary.BigEndian.Uint32(encoded[28:32])
	ownerY := binary.BigEndian.Uint32(encoded[32:36])
	if ownerZ != worldgraph.GlobalRoutingZoom || ownerX >= 1<<worldgraph.GlobalRoutingZoom || ownerY >= 1<<worldgraph.GlobalRoutingZoom {
		return globalNodeRecord{}, fmt.Errorf("%w: invalid global node owner", worldgraph.ErrInvalidChunk)
	}
	record := globalNodeRecord{
		ID:  decodeNodeKey(encoded[0:8]),
		Lon: math.Float64frombits(binary.BigEndian.Uint64(encoded[8:16])),
		Lat: math.Float64frombits(binary.BigEndian.Uint64(encoded[16:24])),
		Owner: worldgraph.TileID{
			Z: int(ownerZ), X: int(ownerX), Y: int(ownerY),
		},
	}
	if err := validateGlobalNodeRecord(record); err != nil {
		return globalNodeRecord{}, err
	}
	return record, nil
}

func validateGlobalNodeRecord(record globalNodeRecord) error {
	if !finiteCoordinate(record.Lon) || record.Lon < -180 || record.Lon > 180 ||
		!finiteCoordinate(record.Lat) || record.Lat < -worldgraph.MaxMercatorLatitude || record.Lat > worldgraph.MaxMercatorLatitude {
		return fmt.Errorf("%w: node %d has invalid coordinates", worldgraph.ErrInvalidChunk, record.ID)
	}
	owner, err := worldgraph.TileForPosition(record.Lon, record.Lat, worldgraph.GlobalRoutingZoom)
	if err != nil || owner != record.Owner {
		return fmt.Errorf("%w: node %d has invalid owner", worldgraph.ErrInvalidChunk, record.ID)
	}
	return nil
}

func writeSelectedGlobalNodes(
	ctx context.Context,
	referencesPath string,
	outputPath string,
	scan func(func([]worldgraph.Node) error) error,
) error {
	if ctx == nil {
		return fmt.Errorf("global node selection context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if scan == nil {
		return fmt.Errorf("global node scanner is nil")
	}
	references, err := os.Open(referencesPath)
	if err != nil {
		return err
	}
	defer references.Close()
	if info, err := references.Stat(); err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("sorted node references are not a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return err
	}
	output, err := os.CreateTemp(filepath.Dir(outputPath), ".nodes-*.tmp")
	if err != nil {
		return err
	}
	temporary := output.Name()
	closed := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
		_ = os.Remove(temporary)
	}()
	if err := output.Chmod(0o600); err != nil {
		return err
	}

	var reference int64
	hasReference := false
	var previousReference int64
	hasPreviousReference := false
	advanceReference := func() error {
		value, ok, err := readInt64Record(references)
		if err != nil {
			return err
		}
		if ok && hasPreviousReference && value <= previousReference {
			return fmt.Errorf("sorted node references are not strictly increasing")
		}
		reference = value
		hasReference = ok
		if ok {
			previousReference = value
			hasPreviousReference = true
		}
		return nil
	}
	if err := advanceReference(); err != nil {
		return err
	}

	var previousNode worldgraph.Node
	hasPreviousNode := false
	consume := func(nodes []worldgraph.Node) error {
		for _, node := range nodes {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !finiteCoordinate(node.Lon) || node.Lon < -180 || node.Lon > 180 ||
				!finiteCoordinate(node.Lat) || node.Lat < -worldgraph.MaxMercatorLatitude || node.Lat > worldgraph.MaxMercatorLatitude {
				return fmt.Errorf("%w: node %d has invalid coordinates", worldgraph.ErrInvalidChunk, node.ID)
			}
			if hasPreviousNode {
				if node.ID < previousNode.ID {
					return fmt.Errorf("PBF node IDs decrease from %d to %d", previousNode.ID, node.ID)
				}
				if node.ID == previousNode.ID {
					if node.Lon != previousNode.Lon || node.Lat != previousNode.Lat {
						return fmt.Errorf("conflicting duplicate PBF node %d", node.ID)
					}
					continue
				}
			}
			previousNode = node
			hasPreviousNode = true
			if hasReference && reference < node.ID {
				return fmt.Errorf("referenced node %d is missing from PBF", reference)
			}
			if !hasReference || reference != node.ID {
				continue
			}
			owner, err := worldgraph.TileForPosition(node.Lon, node.Lat, worldgraph.GlobalRoutingZoom)
			if err != nil {
				return err
			}
			if err := writeGlobalNodeRecord(output, globalNodeRecord{ID: node.ID, Lon: node.Lon, Lat: node.Lat, Owner: owner}); err != nil {
				return err
			}
			if err := advanceReference(); err != nil {
				return err
			}
		}
		return nil
	}
	if err := scan(consume); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if hasReference {
		return fmt.Errorf("referenced node %d is missing from PBF", reference)
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	closed = true
	if err := os.Rename(temporary, outputPath); err != nil {
		return err
	}
	return syncBuilderDirectory(filepath.Dir(outputPath))
}
