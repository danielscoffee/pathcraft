package builder

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	internalosm "github.com/danielscoffee/pathcraft/internal/osm"
	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

const (
	waySpoolVersion         = uint32(1)
	maxWaySpoolPolicyBytes  = 1_024
	maxWaySpoolRecordBytes  = 4 + 8 + 4 + DefaultMaxWayNodes*8 + 10*4 + worldgraph.MaxEdgeHighwayBytes + worldgraph.MaxEdgeNameBytes + 8*maxWaySpoolPolicyBytes
	waySpoolFixedHeaderSize = 4 + 8 + 4
)

var (
	ErrCorruptWaySpool = errors.New("worldgraph way spool is corrupt")
	waySpoolTagKeys    = [...]string{"highway", "name", "oneway", "junction", "access", "foot", "vehicle", "motor_vehicle", "motorcar", "service"}
)

func writeWaySpoolRecord(writer io.Writer, way Way, maxWayNodes int) error {
	if writer == nil {
		return fmt.Errorf("way spool writer is nil")
	}
	if err := validateWaySpoolRecord(way, maxWayNodes); err != nil {
		return err
	}
	var payload bytes.Buffer
	if err := binary.Write(&payload, binary.BigEndian, waySpoolVersion); err != nil {
		return err
	}
	if err := writeFixedRecord(&payload, nodeKey(way.ID)); err != nil {
		return err
	}
	if err := binary.Write(&payload, binary.BigEndian, uint32(len(way.NodeIDs))); err != nil {
		return err
	}
	for _, id := range way.NodeIDs {
		if err := writeFixedRecord(&payload, nodeKey(id)); err != nil {
			return err
		}
	}
	for _, key := range waySpoolTagKeys {
		value := way.Tags[key]
		if err := binary.Write(&payload, binary.BigEndian, uint32(len(value))); err != nil {
			return err
		}
		if err := writeFixedRecord(&payload, []byte(value)); err != nil {
			return err
		}
	}
	if payload.Len() > maxWaySpoolRecordBytes {
		return fmt.Errorf("way %d spool payload exceeds %d bytes", way.ID, maxWaySpoolRecordBytes)
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(payload.Len()))
	if err := writeFixedRecord(writer, length[:]); err != nil {
		return err
	}
	return writeFixedRecord(writer, payload.Bytes())
}

func readWaySpoolRecord(reader io.Reader, maxWayNodes int) (Way, bool, error) {
	if reader == nil {
		return Way{}, false, fmt.Errorf("way spool reader is nil")
	}
	if maxWayNodes < 1 || maxWayNodes > DefaultMaxWayNodes {
		return Way{}, false, fmt.Errorf("maximum way node count must be in 1..%d", DefaultMaxWayNodes)
	}
	var lengthBytes [4]byte
	count, err := io.ReadFull(reader, lengthBytes[:])
	if errors.Is(err, io.EOF) && count == 0 {
		return Way{}, false, nil
	}
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return Way{}, false, fmt.Errorf("%w: %w", ErrCorruptWaySpool, ErrTruncatedFixedRecord)
		}
		return Way{}, false, err
	}
	length := binary.BigEndian.Uint32(lengthBytes[:])
	maximum := waySpoolMaximumBytes(maxWayNodes)
	if length < waySpoolFixedHeaderSize || uint64(length) > uint64(maximum) {
		return Way{}, false, fmt.Errorf("%w: invalid record length %d", ErrCorruptWaySpool, length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return Way{}, false, fmt.Errorf("%w: %w", ErrCorruptWaySpool, ErrTruncatedFixedRecord)
		}
		return Way{}, false, err
	}
	decoded := bytes.NewReader(payload)
	var version uint32
	if err := binary.Read(decoded, binary.BigEndian, &version); err != nil || version != waySpoolVersion {
		return Way{}, false, fmt.Errorf("%w: unsupported version", ErrCorruptWaySpool)
	}
	var idBytes [8]byte
	if _, err := io.ReadFull(decoded, idBytes[:]); err != nil {
		return Way{}, false, fmt.Errorf("%w: way ID", ErrCorruptWaySpool)
	}
	var nodeCount uint32
	if err := binary.Read(decoded, binary.BigEndian, &nodeCount); err != nil || nodeCount < 2 || nodeCount > uint32(maxWayNodes) {
		return Way{}, false, fmt.Errorf("%w: invalid node count %d", ErrCorruptWaySpool, nodeCount)
	}
	way := Way{ID: decodeNodeKey(idBytes[:]), NodeIDs: make([]int64, int(nodeCount)), Tags: make(map[string]string, len(waySpoolTagKeys))}
	for index := range way.NodeIDs {
		if _, err := io.ReadFull(decoded, idBytes[:]); err != nil {
			return Way{}, false, fmt.Errorf("%w: node references", ErrCorruptWaySpool)
		}
		way.NodeIDs[index] = decodeNodeKey(idBytes[:])
	}
	for _, key := range waySpoolTagKeys {
		var stringLength uint32
		if err := binary.Read(decoded, binary.BigEndian, &stringLength); err != nil {
			return Way{}, false, fmt.Errorf("%w: tag length", ErrCorruptWaySpool)
		}
		limit := maxWaySpoolPolicyBytes
		switch key {
		case "highway":
			limit = worldgraph.MaxEdgeHighwayBytes
		case "name":
			limit = worldgraph.MaxEdgeNameBytes
		}
		if stringLength > uint32(limit) || uint64(stringLength) > uint64(decoded.Len()) {
			return Way{}, false, fmt.Errorf("%w: tag %q length %d", ErrCorruptWaySpool, key, stringLength)
		}
		value := make([]byte, int(stringLength))
		if _, err := io.ReadFull(decoded, value); err != nil {
			return Way{}, false, fmt.Errorf("%w: tag %q", ErrCorruptWaySpool, key)
		}
		if len(value) > 0 {
			way.Tags[key] = string(value)
		}
	}
	if decoded.Len() != 0 {
		return Way{}, false, fmt.Errorf("%w: trailing record bytes", ErrCorruptWaySpool)
	}
	if err := validateWaySpoolRecord(way, maxWayNodes); err != nil {
		return Way{}, false, fmt.Errorf("%w: %v", ErrCorruptWaySpool, err)
	}
	return way, true, nil
}

func validateWaySpoolRecord(way Way, maxWayNodes int) error {
	if maxWayNodes < 1 || maxWayNodes > DefaultMaxWayNodes {
		return fmt.Errorf("maximum way node count must be in 1..%d", DefaultMaxWayNodes)
	}
	if len(way.NodeIDs) < 2 || len(way.NodeIDs) > maxWayNodes {
		return fmt.Errorf("way %d has %d nodes, limit 2..%d", way.ID, len(way.NodeIDs), maxWayNodes)
	}
	if len(way.Tags) > maxPBFTagsPerEntity {
		return fmt.Errorf("way %d has too many tags", way.ID)
	}
	for _, key := range waySpoolTagKeys {
		limit := maxWaySpoolPolicyBytes
		switch key {
		case "highway":
			limit = worldgraph.MaxEdgeHighwayBytes
		case "name":
			limit = worldgraph.MaxEdgeNameBytes
		}
		if len(way.Tags[key]) > limit {
			return fmt.Errorf("way %d tag %q exceeds %d bytes", way.ID, key, limit)
		}
	}
	estimatedBytes := int64(len(way.NodeIDs)-1) * 6 * int64(len(way.Tags["highway"])+len(way.Tags["name"])+worldgraph.MaxSourceNameBytes+256)
	if estimatedBytes > maxWayContributionBytes {
		return fmt.Errorf("way %d contributions exceed %d estimated bytes", way.ID, maxWayContributionBytes)
	}
	return nil
}

func waySpoolMaximumBytes(maxWayNodes int) int {
	return 4 + 8 + 4 + maxWayNodes*8 + 10*4 + worldgraph.MaxEdgeHighwayBytes + worldgraph.MaxEdgeNameBytes + 8*maxWaySpoolPolicyBytes
}

func replayWaySpool(ctx context.Context, path string, maxWayNodes int, consume func(Way) error) error {
	if ctx == nil {
		return fmt.Errorf("way spool context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("way spool consumer is nil")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("way spool is not a regular file")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		way, ok, err := readWaySpoolRecord(file, maxWayNodes)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := consume(way); err != nil {
			return err
		}
	}
}

func spoolGlobalWaysAndReferences(ctx context.Context, source *pbfSource, waysPath, referencesPath string) (int64, int64, error) {
	if ctx == nil {
		return 0, 0, fmt.Errorf("global way spool context is nil")
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if source == nil {
		return 0, 0, fmt.Errorf("PBF source is nil")
	}
	for _, path := range []string{waysPath, referencesPath} {
		if path == "" {
			return 0, 0, fmt.Errorf("way and reference spool paths are required")
		}
		if _, err := os.Lstat(path); err == nil {
			return 0, 0, fmt.Errorf("spool output %q already exists", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return 0, 0, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return 0, 0, err
		}
	}
	waysFile, err := os.CreateTemp(filepath.Dir(waysPath), ".ways-*.tmp")
	if err != nil {
		return 0, 0, err
	}
	waysTemporary := waysFile.Name()
	referencesFile, err := os.CreateTemp(filepath.Dir(referencesPath), ".refs-*.tmp")
	if err != nil {
		_ = waysFile.Close()
		_ = os.Remove(waysTemporary)
		return 0, 0, err
	}
	referencesTemporary := referencesFile.Name()
	closed := false
	committed := false
	defer func() {
		if !closed {
			_ = waysFile.Close()
			_ = referencesFile.Close()
		}
		_ = os.Remove(waysTemporary)
		_ = os.Remove(referencesTemporary)
		if !committed {
			_ = os.Remove(waysPath)
			_ = os.Remove(referencesPath)
		}
	}()
	if err := waysFile.Chmod(0o600); err != nil {
		return 0, 0, err
	}
	if err := referencesFile.Chmod(0o600); err != nil {
		return 0, 0, err
	}
	var wayCount, referenceCount int64
	err = scanWaysReader(ctx, source.Reader(), source.maxWayNodes, func(way Way) error {
		policy := internalosm.PolicyForTags(way.Tags)
		if !policy.Routable || len(way.NodeIDs) < 2 {
			return nil
		}
		if err := writeWaySpoolRecord(waysFile, way, source.maxWayNodes); err != nil {
			return err
		}
		for _, id := range way.NodeIDs {
			if err := writeInt64Record(referencesFile, id); err != nil {
				return err
			}
		}
		wayCount++
		referenceCount += int64(len(way.NodeIDs))
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if err := waysFile.Sync(); err != nil {
		return 0, 0, err
	}
	if err := referencesFile.Sync(); err != nil {
		return 0, 0, err
	}
	if err := waysFile.Close(); err != nil {
		return 0, 0, err
	}
	if err := referencesFile.Close(); err != nil {
		return 0, 0, err
	}
	closed = true
	if err := os.Rename(waysTemporary, waysPath); err != nil {
		return 0, 0, err
	}
	if err := os.Rename(referencesTemporary, referencesPath); err != nil {
		return 0, 0, err
	}
	if err := syncBuilderDirectory(filepath.Dir(waysPath)); err != nil {
		return 0, 0, err
	}
	if filepath.Dir(referencesPath) != filepath.Dir(waysPath) {
		if err := syncBuilderDirectory(filepath.Dir(referencesPath)); err != nil {
			return 0, 0, err
		}
	}
	committed = true
	return wayCount, referenceCount, nil
}
