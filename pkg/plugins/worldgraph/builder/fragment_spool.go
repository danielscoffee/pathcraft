package builder

import (
	"bufio"
	"bytes"
	"container/list"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	internalosm "github.com/danielscoffee/pathcraft/internal/osm"
	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

const (
	fragmentSpoolVersion        = uint32(1)
	maxFragmentSegments         = DefaultMaxWayNodes - 1
	fragmentSpoolSegmentBytes   = 2 * (8 + 8 + 8) // ID, longitude, and latitude for each endpoint.
	fragmentSpoolBufferBytes    = 64 << 10
	maxFragmentSpoolRecordBytes = 4 + 8 + 1 +
		2 + worldgraph.MaxEdgeHighwayBytes +
		2 + worldgraph.MaxEdgeNameBytes +
		4 + maxFragmentSegments*fragmentSpoolSegmentBytes
	fragmentSpoolMinimumRecordBytes = 4 + 8 + 1 + 2 + 2 + 4 + fragmentSpoolSegmentBytes
	fragmentPlanetSource            = "planet"
)

var ErrCorruptFragmentSpool = errors.New("worldgraph fragment spool is corrupt")

type globalFragmentSegment struct {
	From worldgraph.Node
	To   worldgraph.Node
}

type globalWayFragment struct {
	WayID           int64
	Highway         string
	Name            string
	Direction       internalosm.Direction
	RestrictWalking bool
	RestrictDriving bool
	Segments        []globalFragmentSegment
}

type openFragmentSpool struct {
	shard  worldgraph.TileID
	path   string
	file   *os.File
	buffer *bufio.Writer
}

type fragmentSpoolWriter struct {
	ctx     context.Context
	root    string
	maxOpen int
	open    map[worldgraph.TileID]*list.Element
	lru     *list.List
	shards  map[worldgraph.TileID]struct{}
	closed  bool
}

func newFragmentSpoolWriter(ctx context.Context, root string, maxOpen int) (*fragmentSpoolWriter, error) {
	if ctx == nil {
		return nil, fmt.Errorf("fragment spool context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if root == "" || maxOpen < 1 {
		return nil, fmt.Errorf("fragment spool root and positive file limit are required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &fragmentSpoolWriter{
		ctx: ctx, root: root, maxOpen: maxOpen,
		open: make(map[worldgraph.TileID]*list.Element),
		lru:  list.New(), shards: make(map[worldgraph.TileID]struct{}),
	}, nil
}

func (writer *fragmentSpoolWriter) Add(shard worldgraph.TileID, fragment globalWayFragment) error {
	if writer == nil || writer.closed {
		return fmt.Errorf("fragment spool writer is closed")
	}
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	record, err := encodeFragmentSpoolRecord(shard, fragment)
	if err != nil {
		return err
	}
	if err := writer.ctx.Err(); err != nil {
		return err
	}

	element := writer.open[shard]
	if element == nil {
		if writer.lru.Len() == writer.maxOpen {
			if err := writer.closeElement(writer.lru.Back()); err != nil {
				return err
			}
		}
		path, err := fragmentSpoolPath(writer.root, shard)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if err := file.Chmod(0o600); err != nil {
			_ = file.Close()
			return err
		}
		spool := &openFragmentSpool{
			shard: shard, path: path, file: file,
			buffer: bufio.NewWriterSize(file, fragmentSpoolBufferBytes),
		}
		element = writer.lru.PushFront(spool)
		writer.open[shard] = element
	} else {
		writer.lru.MoveToFront(element)
	}

	spool := element.Value.(*openFragmentSpool)
	if err := writeFixedRecord(spool.buffer, record); err != nil {
		return errors.Join(err, writer.closeElement(element))
	}
	writer.shards[shard] = struct{}{}
	return nil
}

func (writer *fragmentSpoolWriter) Close() error {
	if writer == nil || writer.closed {
		return nil
	}
	writer.closed = true
	var closeErrors []error
	for writer.lru.Len() > 0 {
		if err := writer.closeElement(writer.lru.Back()); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	if err := writer.ctx.Err(); err != nil {
		closeErrors = append(closeErrors, err)
	}
	return errors.Join(closeErrors...)
}

func (writer *fragmentSpoolWriter) Shards() []worldgraph.TileID {
	if writer == nil {
		return nil
	}
	shards := make([]worldgraph.TileID, 0, len(writer.shards))
	for shard := range writer.shards {
		shards = append(shards, shard)
	}
	sort.Slice(shards, func(i, j int) bool {
		if shards[i].Z != shards[j].Z {
			return shards[i].Z < shards[j].Z
		}
		if shards[i].X != shards[j].X {
			return shards[i].X < shards[j].X
		}
		return shards[i].Y < shards[j].Y
	})
	return shards
}

func (writer *fragmentSpoolWriter) closeElement(element *list.Element) error {
	if element == nil {
		return nil
	}
	spool := element.Value.(*openFragmentSpool)
	delete(writer.open, spool.shard)
	writer.lru.Remove(element)
	flushErr := spool.buffer.Flush()
	syncErr := spool.file.Sync()
	closeErr := spool.file.Close()
	return errors.Join(flushErr, syncErr, closeErr)
}

func fragmentSpoolPath(root string, shard worldgraph.TileID) (string, error) {
	if err := validatePackedBuilderShard(shard); err != nil {
		return "", err
	}
	return filepath.Join(root, "fragments", strconv.Itoa(shard.X>>4), strconv.Itoa(shard.X), strconv.Itoa(shard.Y)+".spool"), nil
}

func writeFragmentSpoolRecord(writer io.Writer, shard worldgraph.TileID, fragment globalWayFragment) error {
	if writer == nil {
		return fmt.Errorf("fragment spool writer is nil")
	}
	record, err := encodeFragmentSpoolRecord(shard, fragment)
	if err != nil {
		return err
	}
	return writeFixedRecord(writer, record)
}

func encodeFragmentSpoolRecord(shard worldgraph.TileID, fragment globalWayFragment) ([]byte, error) {
	if err := validateGlobalWayFragment(shard, fragment); err != nil {
		return nil, err
	}

	var payload bytes.Buffer
	payload.Grow(fragmentSpoolMinimumRecordBytes + len(fragment.Highway) + len(fragment.Name) + (len(fragment.Segments)-1)*fragmentSpoolSegmentBytes)
	if err := binary.Write(&payload, binary.BigEndian, fragmentSpoolVersion); err != nil {
		return nil, err
	}
	if err := writeFixedRecord(&payload, nodeKey(fragment.WayID)); err != nil {
		return nil, err
	}
	policy := uint8(fragment.Direction)
	if fragment.RestrictWalking {
		policy |= 1 << 2
	}
	if fragment.RestrictDriving {
		policy |= 1 << 3
	}
	if err := payload.WriteByte(policy); err != nil {
		return nil, err
	}
	if err := writeShortString(&payload, fragment.Highway); err != nil {
		return nil, err
	}
	if err := writeShortString(&payload, fragment.Name); err != nil {
		return nil, err
	}
	if err := binary.Write(&payload, binary.BigEndian, uint32(len(fragment.Segments))); err != nil {
		return nil, err
	}
	writeNode := func(node worldgraph.Node) error {
		if err := writeFixedRecord(&payload, nodeKey(node.ID)); err != nil {
			return err
		}
		if err := binary.Write(&payload, binary.BigEndian, math.Float64bits(node.Lon)); err != nil {
			return err
		}
		return binary.Write(&payload, binary.BigEndian, math.Float64bits(node.Lat))
	}
	for _, segment := range fragment.Segments {
		if err := writeNode(segment.From); err != nil {
			return nil, err
		}
		if err := writeNode(segment.To); err != nil {
			return nil, err
		}
	}
	if payload.Len() > maxFragmentSpoolRecordBytes {
		return nil, fmt.Errorf("way %d fragment spool payload exceeds %d bytes", fragment.WayID, maxFragmentSpoolRecordBytes)
	}

	record := make([]byte, 4+payload.Len())
	binary.BigEndian.PutUint32(record[:4], uint32(payload.Len()))
	copy(record[4:], payload.Bytes())
	return record, nil
}

func readFragmentSpoolRecord(reader io.Reader, shard worldgraph.TileID) (globalWayFragment, bool, error) {
	if reader == nil {
		return globalWayFragment{}, false, fmt.Errorf("fragment spool reader is nil")
	}
	if err := validatePackedBuilderShard(shard); err != nil {
		return globalWayFragment{}, false, err
	}

	var lengthBytes [4]byte
	count, err := io.ReadFull(reader, lengthBytes[:])
	if errors.Is(err, io.EOF) && count == 0 {
		return globalWayFragment{}, false, nil
	}
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return globalWayFragment{}, false, fmt.Errorf("%w: %w", ErrCorruptFragmentSpool, ErrTruncatedFixedRecord)
		}
		return globalWayFragment{}, false, err
	}
	length := binary.BigEndian.Uint32(lengthBytes[:])
	if length < fragmentSpoolMinimumRecordBytes || uint64(length) > uint64(maxFragmentSpoolRecordBytes) {
		return globalWayFragment{}, false, fmt.Errorf("%w: invalid record length %d", ErrCorruptFragmentSpool, length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return globalWayFragment{}, false, fmt.Errorf("%w: %w", ErrCorruptFragmentSpool, ErrTruncatedFixedRecord)
		}
		return globalWayFragment{}, false, err
	}

	decoded := bytes.NewReader(payload)
	var version uint32
	if err := binary.Read(decoded, binary.BigEndian, &version); err != nil || version != fragmentSpoolVersion {
		return globalWayFragment{}, false, fmt.Errorf("%w: unsupported version", ErrCorruptFragmentSpool)
	}
	var idBytes [8]byte
	if _, err := io.ReadFull(decoded, idBytes[:]); err != nil {
		return globalWayFragment{}, false, fmt.Errorf("%w: way ID", ErrCorruptFragmentSpool)
	}
	fragment := globalWayFragment{WayID: decodeNodeKey(idBytes[:])}
	policy, err := decoded.ReadByte()
	if err != nil || policy&^uint8(0x0f) != 0 || policy&3 == 3 {
		return globalWayFragment{}, false, fmt.Errorf("%w: invalid way policy", ErrCorruptFragmentSpool)
	}
	fragment.Direction = internalosm.Direction(policy & 3)
	fragment.RestrictWalking = policy&(1<<2) != 0
	fragment.RestrictDriving = policy&(1<<3) != 0
	fragment.Highway, err = readShortString(decoded, worldgraph.MaxEdgeHighwayBytes)
	if err != nil {
		return globalWayFragment{}, false, fmt.Errorf("%w: highway", ErrCorruptFragmentSpool)
	}
	fragment.Name, err = readShortString(decoded, worldgraph.MaxEdgeNameBytes)
	if err != nil {
		return globalWayFragment{}, false, fmt.Errorf("%w: name", ErrCorruptFragmentSpool)
	}
	var segmentCount uint32
	if err := binary.Read(decoded, binary.BigEndian, &segmentCount); err != nil || segmentCount == 0 || segmentCount > uint32(maxFragmentSegments) {
		return globalWayFragment{}, false, fmt.Errorf("%w: invalid segment count %d", ErrCorruptFragmentSpool, segmentCount)
	}
	if uint64(segmentCount)*fragmentSpoolSegmentBytes > uint64(decoded.Len()) {
		return globalWayFragment{}, false, fmt.Errorf("%w: truncated segments", ErrCorruptFragmentSpool)
	}
	fragment.Segments = make([]globalFragmentSegment, int(segmentCount))
	readNode := func() (worldgraph.Node, error) {
		if _, err := io.ReadFull(decoded, idBytes[:]); err != nil {
			return worldgraph.Node{}, err
		}
		var lonBits, latBits uint64
		if err := binary.Read(decoded, binary.BigEndian, &lonBits); err != nil {
			return worldgraph.Node{}, err
		}
		if err := binary.Read(decoded, binary.BigEndian, &latBits); err != nil {
			return worldgraph.Node{}, err
		}
		node := worldgraph.Node{
			ID: decodeNodeKey(idBytes[:]), Lon: math.Float64frombits(lonBits), Lat: math.Float64frombits(latBits),
		}
		if !validMercatorPosition(node.Lon, node.Lat) {
			return worldgraph.Node{}, fmt.Errorf("invalid Web Mercator coordinates")
		}
		owner, err := worldgraph.TileForPosition(node.Lon, node.Lat, worldgraph.GlobalRoutingZoom)
		if err != nil {
			return worldgraph.Node{}, err
		}
		node.Owner = owner
		return node, nil
	}
	for index := range fragment.Segments {
		fragment.Segments[index].From, err = readNode()
		if err != nil {
			return globalWayFragment{}, false, fmt.Errorf("%w: segment %d source: %v", ErrCorruptFragmentSpool, index, err)
		}
		fragment.Segments[index].To, err = readNode()
		if err != nil {
			return globalWayFragment{}, false, fmt.Errorf("%w: segment %d target: %v", ErrCorruptFragmentSpool, index, err)
		}
	}
	if decoded.Len() != 0 {
		return globalWayFragment{}, false, fmt.Errorf("%w: trailing record bytes", ErrCorruptFragmentSpool)
	}
	if err := validateGlobalWayFragment(shard, fragment); err != nil {
		return globalWayFragment{}, false, fmt.Errorf("%w: %v", ErrCorruptFragmentSpool, err)
	}
	return fragment, true, nil
}

func validateGlobalWayFragment(shard worldgraph.TileID, fragment globalWayFragment) error {
	if err := validatePackedBuilderShard(shard); err != nil {
		return err
	}
	if fragment.Highway == "" || len(fragment.Highway) > worldgraph.MaxEdgeHighwayBytes || len(fragment.Name) > worldgraph.MaxEdgeNameBytes {
		return fmt.Errorf("way %d fragment has invalid routing text", fragment.WayID)
	}
	if fragment.Direction != internalosm.DirectionBoth && fragment.Direction != internalosm.DirectionForward && fragment.Direction != internalosm.DirectionReverse {
		return fmt.Errorf("way %d fragment has invalid direction %d", fragment.WayID, fragment.Direction)
	}
	if len(fragment.Segments) == 0 || len(fragment.Segments) > maxFragmentSegments {
		return fmt.Errorf("way %d fragment has %d segments, limit 1..%d", fragment.WayID, len(fragment.Segments), maxFragmentSegments)
	}

	nodes := make(map[int64]worldgraph.Node)
	for index, segment := range fragment.Segments {
		if segment.From.ID == segment.To.ID {
			return fmt.Errorf("way %d fragment segment %d repeats node %d", fragment.WayID, index, segment.From.ID)
		}
		for _, node := range []worldgraph.Node{segment.From, segment.To} {
			if !validMercatorPosition(node.Lon, node.Lat) {
				return fmt.Errorf("way %d fragment node %d has invalid Web Mercator coordinates", fragment.WayID, node.ID)
			}
			owner, err := worldgraph.TileForPosition(node.Lon, node.Lat, worldgraph.GlobalRoutingZoom)
			if err != nil || owner != node.Owner {
				return fmt.Errorf("way %d fragment node %d has invalid owner", fragment.WayID, node.ID)
			}
			if previous, exists := nodes[node.ID]; exists && previous != node {
				return fmt.Errorf("way %d fragment has conflicting node %d", fragment.WayID, node.ID)
			}
			nodes[node.ID] = node
		}
		contributes, err := expandFragmentSegment(fragment, segment, shard, nil)
		if err != nil {
			return fmt.Errorf("way %d fragment segment %d: %w", fragment.WayID, index, err)
		}
		if !contributes {
			return fmt.Errorf("way %d fragment segment %d does not contribute to shard %+v", fragment.WayID, index, shard)
		}
	}
	return nil
}

func expandFragmentSegment(
	fragment globalWayFragment,
	segment globalFragmentSegment,
	shard worldgraph.TileID,
	consume func(edgeContribution) error,
) (bool, error) {
	way := Way{ID: fragment.WayID, Tags: map[string]string{"highway": fragment.Highway, "name": fragment.Name}}
	policy := internalosm.WayPolicy{
		Routable: true, Direction: fragment.Direction,
		RestrictWalking: fragment.RestrictWalking, RestrictDriving: fragment.RestrictDriving,
	}
	edges, err := segmentEdges(way, policy, segment.From, segment.To, fragmentPlanetSource, worldgraph.GlobalRoutingZoom)
	if err != nil {
		return false, err
	}
	if len(edges) != 2 {
		return false, fmt.Errorf("segment expansion produced %d directed edges, want 2", len(edges))
	}

	contributes := false
	for _, edge := range edges {
		from, to := segment.From, segment.To
		switch {
		case edge.ID.From == segment.From.ID && edge.ID.To == segment.To.ID:
		case edge.ID.From == segment.To.ID && edge.ID.To == segment.From.ID:
			from, to = to, from
		default:
			return false, fmt.Errorf("segment expansion produced invalid edge endpoints %+v", edge.ID)
		}
		for _, contribution := range edgeContributions(from, to, edge) {
			if err := validateEdgeContribution(contribution); err != nil {
				return false, err
			}
			actual, err := packedBuilderShard(contribution.tile)
			if err != nil {
				return false, err
			}
			if actual != shard {
				continue
			}
			contributes = true
			if consume != nil {
				if err := consume(contribution); err != nil {
					return false, err
				}
			}
		}
	}
	return contributes, nil
}

func replayFragmentSpool(ctx context.Context, path string, shard worldgraph.TileID, consume func(edgeContribution) error) error {
	if ctx == nil {
		return fmt.Errorf("fragment spool replay context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validatePackedBuilderShard(shard); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("fragment spool consumer is nil")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("fragment spool is not a regular file")
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fragment, ok, err := readFragmentSpoolRecord(file, shard)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		for _, segment := range fragment.Segments {
			if err := ctx.Err(); err != nil {
				return err
			}
			contributes, err := expandFragmentSegment(fragment, segment, shard, func(contribution edgeContribution) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				return consume(contribution)
			})
			if err != nil {
				return err
			}
			if !contributes {
				return fmt.Errorf("%w: segment does not contribute to shard %+v", ErrCorruptFragmentSpool, shard)
			}
		}
	}
}

func buildPackedShardFromFragmentSpool(
	ctx context.Context,
	spoolPath string,
	shard worldgraph.TileID,
	stageRoot string,
	maxSegmentBytes int64,
) (int, int64, error) {
	if ctx == nil {
		return 0, 0, fmt.Errorf("packed fragment shard build context is nil")
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if err := validatePackedBuilderShard(shard); err != nil {
		return 0, 0, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(spoolPath), ".fragment-contributions-*.db")
	if err != nil {
		return 0, 0, err
	}
	databasePath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(databasePath)
		return 0, 0, err
	}
	if err := os.Remove(databasePath); err != nil {
		return 0, 0, err
	}
	defer os.Remove(databasePath)

	store, err := openContributionStore(databasePath)
	if err != nil {
		return 0, 0, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = store.Close()
		}
	}()
	batch := make([]edgeContribution, 0, contributionBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := store.PutBatch(ctx, batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	if err := replayFragmentSpool(ctx, spoolPath, shard, func(contribution edgeContribution) error {
		batch = append(batch, contribution)
		if len(batch) == contributionBatchSize {
			return flush()
		}
		return nil
	}); err != nil {
		return 0, 0, err
	}
	if err := flush(); err != nil {
		return 0, 0, err
	}

	tiles, err := store.Tiles(ctx)
	if err != nil {
		return 0, 0, err
	}
	width := 1 << (worldgraph.GlobalRoutingZoom - worldgraph.PackedShardZoom)
	sort.Slice(tiles, func(i, j int) bool {
		leftX, leftY := tiles[i].X&(width-1), tiles[i].Y&(width-1)
		rightX, rightY := tiles[j].X&(width-1), tiles[j].Y&(width-1)
		return leftY*width+leftX < rightY*width+rightX
	})
	position := 0
	var ownedEdges int64
	next := func() (worldgraph.Chunk, bool, error) {
		if position == len(tiles) {
			if err := store.Close(); err != nil {
				return worldgraph.Chunk{}, false, err
			}
			closed = true
			return worldgraph.Chunk{}, false, nil
		}
		tile := tiles[position]
		actual, err := packedBuilderShard(tile)
		if err != nil || actual != shard {
			return worldgraph.Chunk{}, false, fmt.Errorf("fragment spool produced tile %+v outside shard %+v", tile, shard)
		}
		chunk, found, err := store.Chunk(ctx, tile)
		if err != nil {
			return worldgraph.Chunk{}, false, err
		}
		if !found {
			return worldgraph.Chunk{}, false, fmt.Errorf("fragment contribution tile %+v disappeared", tile)
		}
		normalized := normalizeChunk(chunk)
		for _, edge := range normalized.Edges {
			if edge.Owner == tile {
				ownedEdges++
			}
		}
		position++
		return normalized, true, nil
	}
	if err := worldgraph.WritePackedShardFrom(ctx, stageRoot, shard, next, maxSegmentBytes); err != nil {
		return 0, 0, err
	}
	return position, ownedEdges, nil
}
