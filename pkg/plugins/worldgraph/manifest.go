package worldgraph

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	FormatVersion        = 1
	PreprocessingVersion = 1
	PackedLayout         = "packed"
	PackedLayoutVersion  = 1
	manifestFilename     = "manifest.json"
)

type RegionManifest struct {
	Name         string   `json:"name"`
	SourceSHA256 string   `json:"source_sha256"`
	Tiles        []TileID `json:"tiles,omitempty"`
}

type Manifest struct {
	FormatVersion        int              `json:"format_version"`
	PreprocessingVersion int              `json:"preprocessing_version"`
	Generation           string           `json:"generation"`
	Zoom                 int              `json:"zoom"`
	Bounds               Bounds           `json:"bounds"`
	Regions              []RegionManifest `json:"regions"`
	Tiles                []TileID         `json:"tiles,omitempty"`
	BuiltAt              time.Time        `json:"built_at"`
	Layout               string           `json:"layout,omitempty"`
	LayoutVersion        int              `json:"layout_version,omitempty"`
	ShardZoom            int              `json:"shard_zoom,omitempty"`
	Shards               []TileID         `json:"shards,omitempty"`
	SourceBytes          int64            `json:"source_bytes,omitempty"`
}

func prepareManifest(manifest Manifest) (Manifest, error) {
	manifest.FormatVersion = FormatVersion
	manifest.PreprocessingVersion = PreprocessingVersion
	if manifest.Bounds == (Bounds{}) {
		manifest.Bounds = Bounds{
			West:  -180,
			South: -MaxMercatorLatitude,
			East:  180,
			North: MaxMercatorLatitude,
		}
	}
	if manifest.BuiltAt.IsZero() {
		manifest.BuiltAt = time.Now().UTC()
	}
	return normalizeManifest(manifest)
}

func normalizeManifest(manifest Manifest) (Manifest, error) {
	manifest.Tiles = append([]TileID(nil), manifest.Tiles...)
	manifest.Shards = append([]TileID(nil), manifest.Shards...)
	manifest.Regions = append([]RegionManifest(nil), manifest.Regions...)
	for i := range manifest.Regions {
		manifest.Regions[i].SourceSHA256 = strings.ToLower(manifest.Regions[i].SourceSHA256)
		manifest.Regions[i].Tiles = append([]TileID(nil), manifest.Regions[i].Tiles...)
	}
	switch manifest.Layout {
	case "":
		sortTiles(manifest.Tiles)
		for i := range manifest.Regions {
			sortTiles(manifest.Regions[i].Tiles)
		}
	case PackedLayout:
		sortTiles(manifest.Shards)
	}
	sort.Slice(manifest.Regions, func(i, j int) bool {
		return manifest.Regions[i].Name < manifest.Regions[j].Name
	})
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func validateManifest(manifest Manifest) error {
	if manifest.FormatVersion != FormatVersion {
		return fmt.Errorf("%w: manifest format %d, want %d", ErrUnsupportedVersion, manifest.FormatVersion, FormatVersion)
	}
	if manifest.PreprocessingVersion != PreprocessingVersion {
		return fmt.Errorf("%w: preprocessing format %d, want %d", ErrUnsupportedVersion, manifest.PreprocessingVersion, PreprocessingVersion)
	}
	if !safeGeneration(manifest.Generation) {
		return fmt.Errorf("invalid generation %q", manifest.Generation)
	}
	if !finite(manifest.Bounds.West) || !finite(manifest.Bounds.South) || !finite(manifest.Bounds.East) || !finite(manifest.Bounds.North) ||
		manifest.Bounds.West >= manifest.Bounds.East || manifest.Bounds.South >= manifest.Bounds.North ||
		manifest.Bounds.West < -180 || manifest.Bounds.East > 180 ||
		manifest.Bounds.South < -MaxMercatorLatitude || manifest.Bounds.North > MaxMercatorLatitude {
		return fmt.Errorf("invalid manifest bounds %+v", manifest.Bounds)
	}
	if manifest.BuiltAt.IsZero() {
		return fmt.Errorf("manifest build time is missing")
	}
	switch manifest.Layout {
	case "":
		return validateLegacyManifest(manifest)
	case PackedLayout:
		return validatePackedManifest(manifest)
	default:
		return fmt.Errorf("unsupported worldgraph layout %q", manifest.Layout)
	}
}

func validateLegacyManifest(manifest Manifest) error {
	if manifest.LayoutVersion != 0 || manifest.ShardZoom != 0 || len(manifest.Shards) != 0 || manifest.SourceBytes != 0 {
		return fmt.Errorf("legacy manifest contains packed layout fields")
	}
	n, err := tileCount(manifest.Zoom)
	if err != nil {
		return err
	}
	membership := make(map[TileID]struct{}, len(manifest.Tiles))
	for _, tile := range manifest.Tiles {
		if tile.Z != manifest.Zoom {
			return fmt.Errorf("manifest tile %+v uses another zoom", tile)
		}
		if err := validateTile(tile, n); err != nil {
			return err
		}
		if _, exists := membership[tile]; exists {
			return fmt.Errorf("manifest contains duplicate tile %+v", tile)
		}
		membership[tile] = struct{}{}
	}

	regions := make(map[string]struct{}, len(manifest.Regions))
	claimedTiles := make(map[TileID]struct{}, len(manifest.Tiles))
	for _, region := range manifest.Regions {
		if err := validateRegionIdentity(region, regions); err != nil {
			return err
		}
		regionTiles := make(map[TileID]struct{}, len(region.Tiles))
		for _, tile := range region.Tiles {
			if _, exists := membership[tile]; !exists {
				return fmt.Errorf("region %q references uncovered tile %+v", region.Name, tile)
			}
			if _, exists := regionTiles[tile]; exists {
				return fmt.Errorf("region %q contains duplicate tile %+v", region.Name, tile)
			}
			regionTiles[tile] = struct{}{}
			claimedTiles[tile] = struct{}{}
		}
	}
	if len(claimedTiles) != len(membership) {
		return fmt.Errorf("manifest contains tiles with no region provenance")
	}
	return nil
}

func validatePackedManifest(manifest Manifest) error {
	if manifest.LayoutVersion != PackedLayoutVersion {
		return fmt.Errorf("packed layout version %d, want %d", manifest.LayoutVersion, PackedLayoutVersion)
	}
	if manifest.Zoom != GlobalRoutingZoom || manifest.ShardZoom != PackedShardZoom {
		return fmt.Errorf("packed manifest requires routing zoom %d and shard zoom %d", GlobalRoutingZoom, PackedShardZoom)
	}
	if manifest.SourceBytes < 1 {
		return fmt.Errorf("packed manifest source size must be positive")
	}
	if len(manifest.Tiles) != 0 {
		return fmt.Errorf("packed manifest must not contain global tile IDs")
	}
	if len(manifest.Regions) != 1 {
		return fmt.Errorf("packed manifest requires exactly one source snapshot")
	}
	if len(manifest.Regions[0].Tiles) != 0 {
		return fmt.Errorf("packed source snapshot must not contain tile IDs")
	}
	if err := validateRegionIdentity(manifest.Regions[0], make(map[string]struct{}, 1)); err != nil {
		return err
	}
	if len(manifest.Shards) == 0 {
		return fmt.Errorf("packed manifest has no occupied shards")
	}
	n, err := tileCount(PackedShardZoom)
	if err != nil {
		return err
	}
	seen := make(map[TileID]struct{}, len(manifest.Shards))
	for _, shard := range manifest.Shards {
		if shard.Z != PackedShardZoom {
			return fmt.Errorf("packed shard %+v uses another zoom", shard)
		}
		if err := validateTile(shard, n); err != nil {
			return err
		}
		if _, exists := seen[shard]; exists {
			return fmt.Errorf("packed manifest contains duplicate shard %+v", shard)
		}
		seen[shard] = struct{}{}
	}
	return nil
}

func validateRegionIdentity(region RegionManifest, seen map[string]struct{}) error {
	if strings.TrimSpace(region.Name) == "" {
		return fmt.Errorf("manifest contains unnamed region")
	}
	if len(region.Name) > MaxSourceNameBytes {
		return fmt.Errorf("manifest region name exceeds %d bytes", MaxSourceNameBytes)
	}
	if _, exists := seen[region.Name]; exists {
		return fmt.Errorf("manifest contains duplicate region %q", region.Name)
	}
	seen[region.Name] = struct{}{}
	decodedHash, err := hex.DecodeString(region.SourceSHA256)
	if err != nil || len(decodedHash) != 32 {
		return fmt.Errorf("region %q has invalid source SHA-256", region.Name)
	}
	return nil
}

func safeGeneration(generation string) bool {
	if len(generation) == 0 || len(generation) > 128 {
		return false
	}
	for i, char := range generation {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') ||
			(i > 0 && (char == '-' || char == '_' || char == '.')) {
			continue
		}
		return false
	}
	return true
}

func manifestHasTile(manifest Manifest, tile TileID) bool {
	return manifest.Layout == "" && containsTile(manifest.Tiles, tile)
}

func containsTile(tiles []TileID, tile TileID) bool {
	index := sort.Search(len(tiles), func(i int) bool {
		return !tileLess(tiles[i], tile)
	})
	return index < len(tiles) && tiles[index] == tile
}

func sortTiles(tiles []TileID) {
	sort.Slice(tiles, func(i, j int) bool {
		return tileLess(tiles[i], tiles[j])
	})
}

func tileLess(left, right TileID) bool {
	if left.Z != right.Z {
		return left.Z < right.Z
	}
	if left.X != right.X {
		return left.X < right.X
	}
	return left.Y < right.Y
}
