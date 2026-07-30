package worldgraph

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	shardIndexMagic      = "PCGIDX\x00\x00"
	shardIndexVersion    = uint32(1)
	shardIndexEntryBytes = 1 + 2 + 8 + 4 + sha256.Size
	maxEncodedChunkBytes = len(chunkMagic) + 4 + sha256.Size + MaxChunkPayloadBytes
)

type shardIndexEntry struct {
	Slot    uint8
	Segment uint16
	Offset  uint64
	Length  uint32
	SHA256  [sha256.Size]byte
}

type shardIndex struct {
	Shard   TileID
	Entries []shardIndexEntry
}

func encodeShardIndex(writer io.Writer, index shardIndex) error {
	if writer == nil {
		return fmt.Errorf("shard index writer is nil")
	}
	if err := validateShardIndex(index); err != nil {
		return err
	}
	var header [28]byte
	copy(header[:8], shardIndexMagic)
	binary.BigEndian.PutUint32(header[8:12], shardIndexVersion)
	binary.BigEndian.PutUint32(header[12:16], uint32(index.Shard.Z))
	binary.BigEndian.PutUint32(header[16:20], uint32(index.Shard.X))
	binary.BigEndian.PutUint32(header[20:24], uint32(index.Shard.Y))
	binary.BigEndian.PutUint32(header[24:28], uint32(len(index.Entries)))
	if err := writeAll(writer, header[:]); err != nil {
		return err
	}
	var encoded [shardIndexEntryBytes]byte
	for _, entry := range index.Entries {
		encoded[0] = entry.Slot
		binary.BigEndian.PutUint16(encoded[1:3], entry.Segment)
		binary.BigEndian.PutUint64(encoded[3:11], entry.Offset)
		binary.BigEndian.PutUint32(encoded[11:15], entry.Length)
		copy(encoded[15:], entry.SHA256[:])
		if err := writeAll(writer, encoded[:]); err != nil {
			return err
		}
	}
	return nil
}

func decodeShardIndex(reader io.Reader) (shardIndex, error) {
	if reader == nil {
		return shardIndex{}, fmt.Errorf("%w: shard index reader is nil", ErrCorruptIndex)
	}
	var header [28]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return shardIndex{}, fmt.Errorf("%w: read header: %v", ErrCorruptIndex, err)
	}
	if string(header[:8]) != shardIndexMagic {
		return shardIndex{}, fmt.Errorf("%w: invalid magic", ErrCorruptIndex)
	}
	if version := binary.BigEndian.Uint32(header[8:12]); version != shardIndexVersion {
		return shardIndex{}, fmt.Errorf("%w: index version %d", ErrCorruptIndex, version)
	}
	count := binary.BigEndian.Uint32(header[24:28])
	if count > packedShardSlots {
		return shardIndex{}, fmt.Errorf("%w: index has %d entries", ErrCorruptIndex, count)
	}
	index := shardIndex{
		Shard: TileID{
			Z: int(binary.BigEndian.Uint32(header[12:16])),
			X: int(binary.BigEndian.Uint32(header[16:20])),
			Y: int(binary.BigEndian.Uint32(header[20:24])),
		},
		Entries: make([]shardIndexEntry, int(count)),
	}
	var encoded [shardIndexEntryBytes]byte
	for position := range index.Entries {
		if _, err := io.ReadFull(reader, encoded[:]); err != nil {
			return shardIndex{}, fmt.Errorf("%w: read entry %d: %v", ErrCorruptIndex, position, err)
		}
		entry := &index.Entries[position]
		entry.Slot = encoded[0]
		entry.Segment = binary.BigEndian.Uint16(encoded[1:3])
		entry.Offset = binary.BigEndian.Uint64(encoded[3:11])
		entry.Length = binary.BigEndian.Uint32(encoded[11:15])
		copy(entry.SHA256[:], encoded[15:])
	}
	var trailing [1]byte
	if count, err := reader.Read(trailing[:]); count != 0 || err == nil {
		return shardIndex{}, fmt.Errorf("%w: trailing index bytes", ErrCorruptIndex)
	} else if !errors.Is(err, io.EOF) {
		return shardIndex{}, fmt.Errorf("%w: read trailer: %v", ErrCorruptIndex, err)
	}
	if err := validateShardIndex(index); err != nil {
		return shardIndex{}, err
	}
	return index, nil
}

func validateShardIndex(index shardIndex) error {
	if index.Shard.Z != PackedShardZoom {
		return fmt.Errorf("%w: shard zoom %d, want %d", ErrCorruptIndex, index.Shard.Z, PackedShardZoom)
	}
	n, err := tileCount(PackedShardZoom)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptIndex, err)
	}
	if err := validateTile(index.Shard, n); err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptIndex, err)
	}
	if len(index.Entries) > packedShardSlots {
		return fmt.Errorf("%w: index has %d entries", ErrCorruptIndex, len(index.Entries))
	}
	for position, entry := range index.Entries {
		if position > 0 && index.Entries[position-1].Slot >= entry.Slot {
			return fmt.Errorf("%w: shard slots are not strictly sorted", ErrCorruptIndex)
		}
		if entry.Length == 0 || uint64(entry.Length) > uint64(maxEncodedChunkBytes) {
			return fmt.Errorf("%w: invalid chunk length %d", ErrCorruptIndex, entry.Length)
		}
		if entry.Offset > math.MaxUint64-uint64(entry.Length) {
			return fmt.Errorf("%w: chunk range overflows", ErrCorruptIndex)
		}
	}
	return nil
}

func (index shardIndex) entry(slot uint8) (shardIndexEntry, bool) {
	position, found := binarySearchShardEntry(index.Entries, slot)
	if !found {
		return shardIndexEntry{}, false
	}
	return index.Entries[position], true
}

func binarySearchShardEntry(entries []shardIndexEntry, slot uint8) (int, bool) {
	low, high := 0, len(entries)
	for low < high {
		middle := low + (high-low)/2
		if entries[middle].Slot < slot {
			low = middle + 1
		} else {
			high = middle
		}
	}
	return low, low < len(entries) && entries[low].Slot == slot
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}
