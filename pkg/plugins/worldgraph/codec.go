package worldgraph

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"math"
)

const (
	chunkMagic           = "PCGCHNK\x00"
	MaxChunkPayloadBytes = 64 << 20
	MaxChunkNodes        = 250_000
	MaxChunkEdges        = 500_000
	MaxEdgeSources       = 1_024
	MaxEdgeHighwayBytes  = 256
	MaxEdgeNameBytes     = 1_024
	MaxSourceNameBytes   = 256
)

func EncodeChunk(w io.Writer, chunk Chunk) error {
	if err := chunk.validate(); err != nil {
		return err
	}

	var payload bytes.Buffer
	if err := gob.NewEncoder(&payload).Encode(chunk); err != nil {
		return fmt.Errorf("encode worldgraph chunk: %w", err)
	}
	if payload.Len() > MaxChunkPayloadBytes {
		return fmt.Errorf("%w: payload exceeds %d bytes", ErrInvalidChunk, MaxChunkPayloadBytes)
	}
	checksum := sha256.Sum256(payload.Bytes())
	if _, err := io.WriteString(w, chunkMagic); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, uint32(FormatVersion)); err != nil {
		return err
	}
	if _, err := w.Write(checksum[:]); err != nil {
		return err
	}
	if _, err := payload.WriteTo(w); err != nil {
		return err
	}
	return nil
}

func DecodeChunk(r io.Reader) (*Chunk, error) {
	var magic [len(chunkMagic)]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, fmt.Errorf("%w: read header: %w", ErrCorruptChunk, err)
	}
	if string(magic[:]) != chunkMagic {
		return nil, fmt.Errorf("%w: invalid magic", ErrCorruptChunk)
	}

	var version uint32
	if err := binary.Read(r, binary.BigEndian, &version); err != nil {
		return nil, fmt.Errorf("%w: read version: %w", ErrCorruptChunk, err)
	}
	if version != FormatVersion {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedVersion, version, FormatVersion)
	}

	var expected [sha256.Size]byte
	if _, err := io.ReadFull(r, expected[:]); err != nil {
		return nil, fmt.Errorf("%w: read checksum: %w", ErrCorruptChunk, err)
	}
	payload, err := io.ReadAll(io.LimitReader(r, MaxChunkPayloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read payload: %w", ErrCorruptChunk, err)
	}
	if len(payload) > MaxChunkPayloadBytes {
		return nil, fmt.Errorf("%w: payload exceeds %d bytes", ErrCorruptChunk, MaxChunkPayloadBytes)
	}
	actual := sha256.Sum256(payload)
	if !bytes.Equal(actual[:], expected[:]) {
		return nil, ErrChecksumMismatch
	}

	var chunk Chunk
	if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&chunk); err != nil {
		return nil, fmt.Errorf("%w: decode payload: %w", ErrCorruptChunk, err)
	}
	if err := chunk.validate(); err != nil {
		return nil, err
	}
	return &chunk, nil
}

func (chunk *Chunk) validate() error {
	if len(chunk.Nodes) > MaxChunkNodes {
		return fmt.Errorf("%w: chunk has %d nodes, limit %d", ErrInvalidChunk, len(chunk.Nodes), MaxChunkNodes)
	}
	if len(chunk.Edges) > MaxChunkEdges {
		return fmt.Errorf("%w: chunk has %d edges, limit %d", ErrInvalidChunk, len(chunk.Edges), MaxChunkEdges)
	}
	n, err := tileCount(chunk.Tile.Z)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChunk, err)
	}
	if err := validateTile(chunk.Tile, n); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChunk, err)
	}

	nodes := make(map[int64]Node, len(chunk.Nodes))
	for _, node := range chunk.Nodes {
		if _, exists := nodes[node.ID]; exists {
			return fmt.Errorf("%w: duplicate node %d", ErrInvalidChunk, node.ID)
		}
		if !finite(node.Lon) || node.Lon < -180 || node.Lon > 180 || !finite(node.Lat) || node.Lat < -MaxMercatorLatitude || node.Lat > MaxMercatorLatitude {
			return fmt.Errorf("%w: node %d has invalid coordinates", ErrInvalidChunk, node.ID)
		}
		if node.Owner.Z != chunk.Tile.Z {
			return fmt.Errorf("%w: node %d owner uses another zoom", ErrInvalidChunk, node.ID)
		}
		if err := validateTile(node.Owner, n); err != nil {
			return fmt.Errorf("%w: node %d owner: %v", ErrInvalidChunk, node.ID, err)
		}
		owner, err := TileForPosition(node.Lon, node.Lat, node.Owner.Z)
		if err != nil || owner != node.Owner {
			return fmt.Errorf("%w: node %d has incorrect owner", ErrInvalidChunk, node.ID)
		}
		nodes[node.ID] = node
	}

	edges := make(map[EdgeID]struct{}, len(chunk.Edges))
	for _, edge := range chunk.Edges {
		if _, exists := edges[edge.ID]; exists {
			return fmt.Errorf("%w: duplicate edge %+v", ErrInvalidChunk, edge.ID)
		}
		from, fromExists := nodes[edge.ID.From]
		if !fromExists {
			return fmt.Errorf("%w: edge %+v source node is missing", ErrInvalidChunk, edge.ID)
		}
		to, toExists := nodes[edge.ID.To]
		if !toExists {
			return fmt.Errorf("%w: edge %+v target node is missing", ErrInvalidChunk, edge.ID)
		}
		if edge.ID.From == edge.ID.To || !finite(edge.DistanceMeters) || edge.DistanceMeters < 0 {
			return fmt.Errorf("%w: edge %+v has invalid distance or endpoints", ErrInvalidChunk, edge.ID)
		}
		if edge.Owner.Z != chunk.Tile.Z {
			return fmt.Errorf("%w: edge %+v owner uses another zoom", ErrInvalidChunk, edge.ID)
		}
		if err := validateTile(edge.Owner, n); err != nil {
			return fmt.Errorf("%w: edge %+v owner: %v", ErrInvalidChunk, edge.ID, err)
		}
		expectedOwner, err := TileForEdge(from, to, chunk.Tile.Z)
		if err != nil || edge.Owner != expectedOwner {
			return fmt.Errorf("%w: edge %+v has incorrect owner", ErrInvalidChunk, edge.ID)
		}
		if len(edge.Highway) > MaxEdgeHighwayBytes || len(edge.Name) > MaxEdgeNameBytes {
			return fmt.Errorf("%w: edge %+v text exceeds limits", ErrInvalidChunk, edge.ID)
		}
		if len(edge.Sources) == 0 {
			return fmt.Errorf("%w: edge %+v has no sources", ErrInvalidChunk, edge.ID)
		}
		if len(edge.Sources) > MaxEdgeSources {
			return fmt.Errorf("%w: edge %+v has %d sources, limit %d", ErrInvalidChunk, edge.ID, len(edge.Sources), MaxEdgeSources)
		}
		sources := make(map[string]struct{}, len(edge.Sources))
		for _, source := range edge.Sources {
			if source == "" {
				return fmt.Errorf("%w: edge %+v has empty source", ErrInvalidChunk, edge.ID)
			}
			if len(source) > MaxSourceNameBytes {
				return fmt.Errorf("%w: edge %+v source exceeds %d bytes", ErrInvalidChunk, edge.ID, MaxSourceNameBytes)
			}
			if _, exists := sources[source]; exists {
				return fmt.Errorf("%w: edge %+v has duplicate source %q", ErrInvalidChunk, edge.ID, source)
			}
			sources[source] = struct{}{}
		}
		edges[edge.ID] = struct{}{}
	}
	return nil
}

func TileForEdge(from, to Node, zoom int) (TileID, error) {
	deltaLon := to.Lon - from.Lon
	if math.Abs(deltaLon) > 180 {
		if deltaLon > 0 {
			deltaLon -= 360
		} else {
			deltaLon += 360
		}
	}
	return TileForPosition(from.Lon+deltaLon/2, (from.Lat+to.Lat)/2, zoom)
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
