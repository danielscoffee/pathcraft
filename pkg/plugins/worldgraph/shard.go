package worldgraph

import "fmt"

const (
	GlobalRoutingZoom = 12
	PackedShardZoom   = 8
	packedShardWidth  = 1 << (GlobalRoutingZoom - PackedShardZoom)
	packedShardSlots  = packedShardWidth * packedShardWidth
)

type shardAddress struct {
	Shard TileID
	Slot  uint8
}

func packedShardAddress(tile TileID) (shardAddress, error) {
	if tile.Z != GlobalRoutingZoom {
		return shardAddress{}, fmt.Errorf("%w: packed tile zoom %d, want %d", ErrInvalidChunk, tile.Z, GlobalRoutingZoom)
	}
	n, err := tileCount(GlobalRoutingZoom)
	if err != nil {
		return shardAddress{}, fmt.Errorf("%w: %v", ErrInvalidChunk, err)
	}
	if err := validateTile(tile, n); err != nil {
		return shardAddress{}, fmt.Errorf("%w: %v", ErrInvalidChunk, err)
	}
	localX := tile.X % packedShardWidth
	localY := tile.Y % packedShardWidth
	return shardAddress{
		Shard: TileID{Z: PackedShardZoom, X: tile.X / packedShardWidth, Y: tile.Y / packedShardWidth},
		Slot:  uint8(localY*packedShardWidth + localX),
	}, nil
}

func packedShardTile(shard TileID, slot uint8) (TileID, error) {
	if shard.Z != PackedShardZoom {
		return TileID{}, fmt.Errorf("%w: packed shard zoom %d, want %d", ErrInvalidChunk, shard.Z, PackedShardZoom)
	}
	n, err := tileCount(PackedShardZoom)
	if err != nil {
		return TileID{}, fmt.Errorf("%w: %v", ErrInvalidChunk, err)
	}
	if err := validateTile(shard, n); err != nil {
		return TileID{}, fmt.Errorf("%w: %v", ErrInvalidChunk, err)
	}
	localX := int(slot) % packedShardWidth
	localY := int(slot) / packedShardWidth
	return TileID{
		Z: GlobalRoutingZoom,
		X: shard.X*packedShardWidth + localX,
		Y: shard.Y*packedShardWidth + localY,
	}, nil
}
