package worldgraph

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestPackedShardAddressRoundTrip(t *testing.T) {
	tests := []struct {
		tile  TileID
		shard TileID
		slot  uint8
	}{
		{tile: TileID{Z: 12, X: 0, Y: 0}, shard: TileID{Z: 8, X: 0, Y: 0}, slot: 0},
		{tile: TileID{Z: 12, X: 15, Y: 15}, shard: TileID{Z: 8, X: 0, Y: 0}, slot: 255},
		{tile: TileID{Z: 12, X: 16, Y: 0}, shard: TileID{Z: 8, X: 1, Y: 0}, slot: 0},
		{tile: TileID{Z: 12, X: 4095, Y: 4095}, shard: TileID{Z: 8, X: 255, Y: 255}, slot: 255},
	}
	for _, test := range tests {
		address, err := packedShardAddress(test.tile)
		if err != nil {
			t.Fatalf("packedShardAddress(%+v) error = %v", test.tile, err)
		}
		if address.Shard != test.shard || address.Slot != test.slot {
			t.Fatalf("packedShardAddress(%+v) = %+v, want shard %+v slot %d", test.tile, address, test.shard, test.slot)
		}
		tile, err := packedShardTile(address.Shard, address.Slot)
		if err != nil {
			t.Fatalf("packedShardTile(%+v, %d) error = %v", address.Shard, address.Slot, err)
		}
		if tile != test.tile {
			t.Fatalf("packedShardTile(%+v, %d) = %+v, want %+v", address.Shard, address.Slot, tile, test.tile)
		}
	}
}

func TestPackedShardAddressRejectsWrongZoom(t *testing.T) {
	if _, err := packedShardAddress(TileID{Z: 11}); !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("packedShardAddress() error = %v, want ErrInvalidChunk", err)
	}
	if _, err := packedShardTile(TileID{Z: 7}, 0); !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("packedShardTile() error = %v, want ErrInvalidChunk", err)
	}
}

func TestShardIndexRoundTrip(t *testing.T) {
	first := sha256.Sum256([]byte("first"))
	last := sha256.Sum256([]byte("last"))
	want := shardIndex{
		Shard: TileID{Z: PackedShardZoom, X: 7, Y: 9},
		Entries: []shardIndexEntry{
			{Slot: 0, Segment: 0, Offset: 0, Length: 123, SHA256: first},
			{Slot: 255, Segment: 3, Offset: 1 << 30, Length: 456, SHA256: last},
		},
	}
	var encoded bytes.Buffer
	if err := encodeShardIndex(&encoded, want); err != nil {
		t.Fatal(err)
	}
	got, err := decodeShardIndex(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decodeShardIndex() = %+v, want %+v", got, want)
	}
}

func TestShardIndexRejectsInvalidEntries(t *testing.T) {
	digest := sha256.Sum256([]byte("chunk"))
	tests := []struct {
		name  string
		index shardIndex
	}{
		{
			name:  "invalid shard",
			index: shardIndex{Shard: TileID{Z: PackedShardZoom - 1}},
		},
		{
			name: "duplicate slot",
			index: shardIndex{Shard: TileID{Z: PackedShardZoom}, Entries: []shardIndexEntry{
				{Slot: 1, Length: 1, SHA256: digest}, {Slot: 1, Length: 1, SHA256: digest},
			}},
		},
		{
			name: "unsorted slot",
			index: shardIndex{Shard: TileID{Z: PackedShardZoom}, Entries: []shardIndexEntry{
				{Slot: 2, Length: 1, SHA256: digest}, {Slot: 1, Length: 1, SHA256: digest},
			}},
		},
		{
			name: "zero length",
			index: shardIndex{Shard: TileID{Z: PackedShardZoom}, Entries: []shardIndexEntry{
				{Slot: 1, Length: 0, SHA256: digest},
			}},
		},
		{
			name: "oversized length",
			index: shardIndex{Shard: TileID{Z: PackedShardZoom}, Entries: []shardIndexEntry{
				{Slot: 1, Length: uint32(maxEncodedChunkBytes + 1), SHA256: digest},
			}},
		},
		{
			name: "range overflow",
			index: shardIndex{Shard: TileID{Z: PackedShardZoom}, Entries: []shardIndexEntry{
				{Slot: 1, Offset: ^uint64(0), Length: 1, SHA256: digest},
			}},
		},
		{
			name: "range exceeds file offset",
			index: shardIndex{Shard: TileID{Z: PackedShardZoom}, Entries: []shardIndexEntry{
				{Slot: 1, Offset: math.MaxInt64, Length: 2, SHA256: digest},
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := encodeShardIndex(new(bytes.Buffer), test.index); !errors.Is(err, ErrCorruptIndex) {
				t.Fatalf("encodeShardIndex() error = %v, want ErrCorruptIndex", err)
			}
		})
	}
}

func TestShardIndexRejectsMalformedEncoding(t *testing.T) {
	digest := sha256.Sum256([]byte("chunk"))
	valid := shardIndex{Shard: TileID{Z: PackedShardZoom}, Entries: []shardIndexEntry{{Slot: 1, Length: 10, SHA256: digest}}}
	var buffer bytes.Buffer
	if err := encodeShardIndex(&buffer, valid); err != nil {
		t.Fatal(err)
	}
	encoded := buffer.Bytes()

	tests := []struct {
		name string
		data []byte
	}{
		{name: "truncated", data: encoded[:len(encoded)-1]},
		{name: "trailing", data: append(append([]byte(nil), encoded...), 0)},
		{name: "wrong magic", data: append([]byte("BADMAGIC"), encoded[len(shardIndexMagic):]...)},
	}

	wrongVersion := append([]byte(nil), encoded...)
	binary.BigEndian.PutUint32(wrongVersion[len(shardIndexMagic):], shardIndexVersion+1)
	tests = append(tests, struct {
		name string
		data []byte
	}{name: "wrong version", data: wrongVersion})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeShardIndex(bytes.NewReader(test.data)); !errors.Is(err, ErrCorruptIndex) {
				t.Fatalf("decodeShardIndex() error = %v, want ErrCorruptIndex", err)
			}
		})
	}
}
