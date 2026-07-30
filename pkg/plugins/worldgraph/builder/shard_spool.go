package builder

import (
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

	"github.com/danielscoffee/pathcraft/pkg/plugins/worldgraph"
)

const (
	shardSpoolVersion        = uint32(1)
	maxShardSpoolRecordBytes = 4 + 12 + 2*36 + 24 + 8 + 2 + worldgraph.MaxEdgeHighwayBytes + 2 + worldgraph.MaxEdgeNameBytes + 1 + 12 + 2 + worldgraph.MaxEdgeSources*(2+worldgraph.MaxSourceNameBytes)
)

var ErrCorruptShardSpool = errors.New("worldgraph shard spool is corrupt")

type openShardSpool struct {
	shard worldgraph.TileID
	path  string
	file  *os.File
}

type shardSpoolWriter struct {
	ctx     context.Context
	root    string
	maxOpen int
	open    map[worldgraph.TileID]*list.Element
	lru     *list.List
	shards  map[worldgraph.TileID]struct{}
	closed  bool
}

func newShardSpoolWriter(ctx context.Context, root string, maxOpen int) (*shardSpoolWriter, error) {
	if ctx == nil {
		return nil, fmt.Errorf("shard spool context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if root == "" || maxOpen < 1 {
		return nil, fmt.Errorf("shard spool root and positive file limit are required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &shardSpoolWriter{
		ctx: ctx, root: root, maxOpen: maxOpen,
		open: make(map[worldgraph.TileID]*list.Element), lru: list.New(), shards: make(map[worldgraph.TileID]struct{}),
	}, nil
}

func (writer *shardSpoolWriter) Add(contribution edgeContribution) error {
	if writer == nil || writer.closed {
		return fmt.Errorf("shard spool writer is closed")
	}
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	if err := validateEdgeContribution(contribution); err != nil {
		return err
	}
	shard, err := packedBuilderShard(contribution.tile)
	if err != nil {
		return err
	}
	element := writer.open[shard]
	if element == nil {
		if writer.lru.Len() == writer.maxOpen {
			if err := writer.closeElement(writer.lru.Back()); err != nil {
				return err
			}
		}
		path, err := shardSpoolPath(writer.root, shard)
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
		element = writer.lru.PushFront(&openShardSpool{shard: shard, path: path, file: file})
		writer.open[shard] = element
		writer.shards[shard] = struct{}{}
	} else {
		writer.lru.MoveToFront(element)
	}
	spool := element.Value.(*openShardSpool)
	if err := writeShardSpoolRecord(spool.file, contribution); err != nil {
		_ = writer.closeElement(element)
		return err
	}
	return nil
}

func (writer *shardSpoolWriter) Close() error {
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

func (writer *shardSpoolWriter) Shards() []worldgraph.TileID {
	if writer == nil {
		return nil
	}
	shards := make([]worldgraph.TileID, 0, len(writer.shards))
	for shard := range writer.shards {
		shards = append(shards, shard)
	}
	sort.Slice(shards, func(i, j int) bool {
		if shards[i].X != shards[j].X {
			return shards[i].X < shards[j].X
		}
		return shards[i].Y < shards[j].Y
	})
	return shards
}

func (writer *shardSpoolWriter) closeElement(element *list.Element) error {
	if element == nil {
		return nil
	}
	spool := element.Value.(*openShardSpool)
	delete(writer.open, spool.shard)
	writer.lru.Remove(element)
	syncErr := spool.file.Sync()
	closeErr := spool.file.Close()
	return errors.Join(syncErr, closeErr)
}

func shardSpoolPath(root string, shard worldgraph.TileID) (string, error) {
	if err := validatePackedBuilderShard(shard); err != nil {
		return "", err
	}
	return filepath.Join(root, "shards", strconv.Itoa(shard.X>>4), strconv.Itoa(shard.X), strconv.Itoa(shard.Y)+".spool"), nil
}

func replayShardSpool(ctx context.Context, path string, shard worldgraph.TileID, consume func(edgeContribution) error) error {
	if ctx == nil {
		return fmt.Errorf("shard spool replay context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validatePackedBuilderShard(shard); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("shard spool consumer is nil")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("shard spool is not a regular file")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		contribution, ok, err := readShardSpoolRecord(file)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		actual, err := packedBuilderShard(contribution.tile)
		if err != nil {
			return err
		}
		if actual != shard {
			return fmt.Errorf("%w: tile %+v belongs to shard %+v, not %+v", ErrCorruptShardSpool, contribution.tile, actual, shard)
		}
		if err := consume(contribution); err != nil {
			return err
		}
	}
}

func writeShardSpoolRecord(writer io.Writer, contribution edgeContribution) error {
	if err := validateEdgeContribution(contribution); err != nil {
		return err
	}
	var payload bytes.Buffer
	if err := binary.Write(&payload, binary.BigEndian, shardSpoolVersion); err != nil {
		return err
	}
	writeTile := func(tile worldgraph.TileID) error {
		for _, value := range []int{tile.Z, tile.X, tile.Y} {
			if err := binary.Write(&payload, binary.BigEndian, uint32(value)); err != nil {
				return err
			}
		}
		return nil
	}
	writeNode := func(node worldgraph.Node) error {
		if err := writeFixedRecord(&payload, nodeKey(node.ID)); err != nil {
			return err
		}
		if err := binary.Write(&payload, binary.BigEndian, math.Float64bits(node.Lon)); err != nil {
			return err
		}
		if err := binary.Write(&payload, binary.BigEndian, math.Float64bits(node.Lat)); err != nil {
			return err
		}
		return writeTile(node.Owner)
	}
	if err := writeTile(contribution.tile); err != nil {
		return err
	}
	if err := writeNode(contribution.from); err != nil {
		return err
	}
	if err := writeNode(contribution.to); err != nil {
		return err
	}
	for _, value := range []int64{contribution.edge.ID.WayID, contribution.edge.ID.From, contribution.edge.ID.To} {
		if err := writeFixedRecord(&payload, nodeKey(value)); err != nil {
			return err
		}
	}
	if err := binary.Write(&payload, binary.BigEndian, math.Float64bits(contribution.edge.DistanceMeters)); err != nil {
		return err
	}
	if err := writeShortString(&payload, contribution.edge.Highway); err != nil {
		return err
	}
	if err := writeShortString(&payload, contribution.edge.Name); err != nil {
		return err
	}
	var flags uint8
	if contribution.edge.RestrictWalking {
		flags |= 1
	}
	if contribution.edge.RestrictDriving {
		flags |= 2
	}
	if err := payload.WriteByte(flags); err != nil {
		return err
	}
	if err := writeTile(contribution.edge.Owner); err != nil {
		return err
	}
	if err := binary.Write(&payload, binary.BigEndian, uint16(len(contribution.edge.Sources))); err != nil {
		return err
	}
	for _, source := range contribution.edge.Sources {
		if err := writeShortString(&payload, source); err != nil {
			return err
		}
	}
	if payload.Len() > maxShardSpoolRecordBytes {
		return fmt.Errorf("edge contribution spool record exceeds %d bytes", maxShardSpoolRecordBytes)
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(payload.Len()))
	if err := writeFixedRecord(writer, length[:]); err != nil {
		return err
	}
	return writeFixedRecord(writer, payload.Bytes())
}

func readShardSpoolRecord(reader io.Reader) (edgeContribution, bool, error) {
	var lengthBytes [4]byte
	count, err := io.ReadFull(reader, lengthBytes[:])
	if errors.Is(err, io.EOF) && count == 0 {
		return edgeContribution{}, false, nil
	}
	if err != nil {
		return edgeContribution{}, false, fmt.Errorf("%w: %w", ErrCorruptShardSpool, ErrTruncatedFixedRecord)
	}
	length := binary.BigEndian.Uint32(lengthBytes[:])
	if length == 0 || uint64(length) > uint64(maxShardSpoolRecordBytes) {
		return edgeContribution{}, false, fmt.Errorf("%w: invalid record length %d", ErrCorruptShardSpool, length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return edgeContribution{}, false, fmt.Errorf("%w: %w", ErrCorruptShardSpool, ErrTruncatedFixedRecord)
	}
	decoded := bytes.NewReader(payload)
	var version uint32
	if err := binary.Read(decoded, binary.BigEndian, &version); err != nil || version != shardSpoolVersion {
		return edgeContribution{}, false, fmt.Errorf("%w: unsupported version", ErrCorruptShardSpool)
	}
	readTile := func() (worldgraph.TileID, error) {
		var values [3]uint32
		for index := range values {
			if err := binary.Read(decoded, binary.BigEndian, &values[index]); err != nil {
				return worldgraph.TileID{}, err
			}
		}
		return worldgraph.TileID{Z: int(values[0]), X: int(values[1]), Y: int(values[2])}, nil
	}
	readNode := func() (worldgraph.Node, error) {
		var id [8]byte
		if _, err := io.ReadFull(decoded, id[:]); err != nil {
			return worldgraph.Node{}, err
		}
		var lonBits, latBits uint64
		if err := binary.Read(decoded, binary.BigEndian, &lonBits); err != nil {
			return worldgraph.Node{}, err
		}
		if err := binary.Read(decoded, binary.BigEndian, &latBits); err != nil {
			return worldgraph.Node{}, err
		}
		owner, err := readTile()
		if err != nil {
			return worldgraph.Node{}, err
		}
		return worldgraph.Node{ID: decodeNodeKey(id[:]), Lon: math.Float64frombits(lonBits), Lat: math.Float64frombits(latBits), Owner: owner}, nil
	}
	contribution := edgeContribution{}
	contribution.tile, err = readTile()
	if err != nil {
		return edgeContribution{}, false, fmt.Errorf("%w: tile", ErrCorruptShardSpool)
	}
	contribution.from, err = readNode()
	if err != nil {
		return edgeContribution{}, false, fmt.Errorf("%w: source node", ErrCorruptShardSpool)
	}
	contribution.to, err = readNode()
	if err != nil {
		return edgeContribution{}, false, fmt.Errorf("%w: target node", ErrCorruptShardSpool)
	}
	ids := []*int64{&contribution.edge.ID.WayID, &contribution.edge.ID.From, &contribution.edge.ID.To}
	var id [8]byte
	for _, target := range ids {
		if _, err := io.ReadFull(decoded, id[:]); err != nil {
			return edgeContribution{}, false, fmt.Errorf("%w: edge ID", ErrCorruptShardSpool)
		}
		*target = decodeNodeKey(id[:])
	}
	var distanceBits uint64
	if err := binary.Read(decoded, binary.BigEndian, &distanceBits); err != nil {
		return edgeContribution{}, false, fmt.Errorf("%w: edge distance", ErrCorruptShardSpool)
	}
	contribution.edge.DistanceMeters = math.Float64frombits(distanceBits)
	contribution.edge.Highway, err = readShortString(decoded, worldgraph.MaxEdgeHighwayBytes)
	if err != nil {
		return edgeContribution{}, false, fmt.Errorf("%w: highway", ErrCorruptShardSpool)
	}
	contribution.edge.Name, err = readShortString(decoded, worldgraph.MaxEdgeNameBytes)
	if err != nil {
		return edgeContribution{}, false, fmt.Errorf("%w: name", ErrCorruptShardSpool)
	}
	flags, err := decoded.ReadByte()
	if err != nil || flags&^uint8(3) != 0 {
		return edgeContribution{}, false, fmt.Errorf("%w: edge flags", ErrCorruptShardSpool)
	}
	contribution.edge.RestrictWalking = flags&1 != 0
	contribution.edge.RestrictDriving = flags&2 != 0
	contribution.edge.Owner, err = readTile()
	if err != nil {
		return edgeContribution{}, false, fmt.Errorf("%w: edge owner", ErrCorruptShardSpool)
	}
	var sourceCount uint16
	if err := binary.Read(decoded, binary.BigEndian, &sourceCount); err != nil || sourceCount == 0 || int(sourceCount) > worldgraph.MaxEdgeSources {
		return edgeContribution{}, false, fmt.Errorf("%w: edge source count", ErrCorruptShardSpool)
	}
	contribution.edge.Sources = make([]string, int(sourceCount))
	for index := range contribution.edge.Sources {
		contribution.edge.Sources[index], err = readShortString(decoded, worldgraph.MaxSourceNameBytes)
		if err != nil {
			return edgeContribution{}, false, fmt.Errorf("%w: edge source", ErrCorruptShardSpool)
		}
	}
	if decoded.Len() != 0 {
		return edgeContribution{}, false, fmt.Errorf("%w: trailing record bytes", ErrCorruptShardSpool)
	}
	if err := validateEdgeContribution(contribution); err != nil {
		return edgeContribution{}, false, fmt.Errorf("%w: %v", ErrCorruptShardSpool, err)
	}
	return contribution, true, nil
}

func writeShortString(writer io.Writer, value string) error {
	if len(value) > math.MaxUint16 {
		return fmt.Errorf("string exceeds uint16 length")
	}
	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(value)))
	if err := writeFixedRecord(writer, length[:]); err != nil {
		return err
	}
	return writeFixedRecord(writer, []byte(value))
}

func readShortString(reader *bytes.Reader, limit int) (string, error) {
	var length uint16
	if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
		return "", err
	}
	if int(length) > limit || int(length) > reader.Len() {
		return "", fmt.Errorf("invalid string length %d", length)
	}
	value := make([]byte, int(length))
	if _, err := io.ReadFull(reader, value); err != nil {
		return "", err
	}
	return string(value), nil
}

func validateEdgeContribution(contribution edgeContribution) error {
	if contribution.tile.Z != worldgraph.GlobalRoutingZoom {
		return fmt.Errorf("contribution tile uses zoom %d", contribution.tile.Z)
	}
	if _, err := packedBuilderShard(contribution.tile); err != nil {
		return err
	}
	for _, node := range []worldgraph.Node{contribution.from, contribution.to} {
		if err := validateGlobalNodeRecord(globalNodeRecord{ID: node.ID, Lon: node.Lon, Lat: node.Lat, Owner: node.Owner}); err != nil {
			return err
		}
	}
	edge := contribution.edge
	if edge.ID.From != contribution.from.ID || edge.ID.To != contribution.to.ID || edge.ID.From == edge.ID.To ||
		!finiteCoordinate(edge.DistanceMeters) || edge.DistanceMeters < 0 {
		return fmt.Errorf("edge contribution has invalid endpoints or distance")
	}
	owner, err := worldgraph.TileForEdge(contribution.from, contribution.to, worldgraph.GlobalRoutingZoom)
	if err != nil || owner != edge.Owner {
		return fmt.Errorf("edge contribution has invalid owner")
	}
	if contribution.tile != contribution.from.Owner && contribution.tile != contribution.to.Owner && contribution.tile != edge.Owner {
		return fmt.Errorf("edge contribution tile is outside ownership halo")
	}
	if len(edge.Highway) > worldgraph.MaxEdgeHighwayBytes || len(edge.Name) > worldgraph.MaxEdgeNameBytes || len(edge.Sources) == 0 || len(edge.Sources) > worldgraph.MaxEdgeSources {
		return fmt.Errorf("edge contribution text or sources exceed limits")
	}
	seen := make(map[string]struct{}, len(edge.Sources))
	for _, source := range edge.Sources {
		if source == "" || len(source) > worldgraph.MaxSourceNameBytes {
			return fmt.Errorf("edge contribution has invalid source")
		}
		if _, exists := seen[source]; exists {
			return fmt.Errorf("edge contribution has duplicate source %q", source)
		}
		seen[source] = struct{}{}
	}
	return nil
}

func packedBuilderShard(tile worldgraph.TileID) (worldgraph.TileID, error) {
	if tile.Z != worldgraph.GlobalRoutingZoom || tile.X < 0 || tile.X >= 1<<worldgraph.GlobalRoutingZoom || tile.Y < 0 || tile.Y >= 1<<worldgraph.GlobalRoutingZoom {
		return worldgraph.TileID{}, fmt.Errorf("invalid global routing tile %+v", tile)
	}
	shift := worldgraph.GlobalRoutingZoom - worldgraph.PackedShardZoom
	return worldgraph.TileID{Z: worldgraph.PackedShardZoom, X: tile.X >> shift, Y: tile.Y >> shift}, nil
}

func validatePackedBuilderShard(shard worldgraph.TileID) error {
	if shard.Z != worldgraph.PackedShardZoom || shard.X < 0 || shard.X >= 1<<worldgraph.PackedShardZoom || shard.Y < 0 || shard.Y >= 1<<worldgraph.PackedShardZoom {
		return fmt.Errorf("invalid packed shard %+v", shard)
	}
	return nil
}

func partitionGlobalContributions(
	ctx context.Context,
	waySpoolPath string,
	nodes *globalNodeIndex,
	spoolRoot string,
	source string,
	maxOpen int,
	maxWayNodes int,
) ([]worldgraph.TileID, int64, error) {
	if ctx == nil {
		return nil, 0, fmt.Errorf("global contribution context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if nodes == nil || source == "" || len(source) > worldgraph.MaxSourceNameBytes {
		return nil, 0, fmt.Errorf("global contribution node index and source are required")
	}
	writer, err := newShardSpoolWriter(ctx, spoolRoot, maxOpen)
	if err != nil {
		return nil, 0, err
	}
	var count int64
	replayErr := replayWaySpool(ctx, waySpoolPath, maxWayNodes, func(way Way) error {
		return addWayContributionsWithLookup(ctx, nodes.Get, func(from, to worldgraph.Node, edge worldgraph.Edge) error {
			for _, contribution := range edgeContributions(from, to, edge) {
				if err := writer.Add(contribution); err != nil {
					return err
				}
				count++
			}
			return nil
		}, way, source, worldgraph.GlobalRoutingZoom)
	})
	closeErr := writer.Close()
	if replayErr != nil || closeErr != nil {
		return nil, 0, errors.Join(replayErr, closeErr)
	}
	return writer.Shards(), count, nil
}

func buildPackedShardFromSpool(
	ctx context.Context,
	spoolPath string,
	shard worldgraph.TileID,
	stageRoot string,
	maxSegmentBytes int64,
) (int, error) {
	if ctx == nil {
		return 0, fmt.Errorf("packed shard build context is nil")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := validatePackedBuilderShard(shard); err != nil {
		return 0, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(spoolPath), ".shard-contributions-*.db")
	if err != nil {
		return 0, err
	}
	databasePath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(databasePath)
		return 0, err
	}
	if err := os.Remove(databasePath); err != nil {
		return 0, err
	}
	defer os.Remove(databasePath)
	store, err := openContributionStore(databasePath)
	if err != nil {
		return 0, err
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
	if err := replayShardSpool(ctx, spoolPath, shard, func(contribution edgeContribution) error {
		batch = append(batch, contribution)
		if len(batch) == contributionBatchSize {
			return flush()
		}
		return nil
	}); err != nil {
		return 0, err
	}
	if err := flush(); err != nil {
		return 0, err
	}
	tiles, err := store.Tiles(ctx)
	if err != nil {
		return 0, err
	}
	width := 1 << (worldgraph.GlobalRoutingZoom - worldgraph.PackedShardZoom)
	sort.Slice(tiles, func(i, j int) bool {
		leftX, leftY := tiles[i].X&(width-1), tiles[i].Y&(width-1)
		rightX, rightY := tiles[j].X&(width-1), tiles[j].Y&(width-1)
		return leftY*width+leftX < rightY*width+rightX
	})
	chunks := make([]worldgraph.Chunk, 0, len(tiles))
	for _, tile := range tiles {
		actual, err := packedBuilderShard(tile)
		if err != nil || actual != shard {
			return 0, fmt.Errorf("shard spool produced tile %+v outside shard %+v", tile, shard)
		}
		chunk, found, err := store.Chunk(ctx, tile)
		if err != nil {
			return 0, err
		}
		if !found {
			return 0, fmt.Errorf("contribution tile %+v disappeared", tile)
		}
		chunks = append(chunks, normalizeChunk(chunk))
	}
	if err := store.Close(); err != nil {
		return 0, err
	}
	closed = true
	if err := worldgraph.WritePackedShard(ctx, stageRoot, shard, chunks, maxSegmentBytes); err != nil {
		return 0, err
	}
	return len(chunks), nil
}
