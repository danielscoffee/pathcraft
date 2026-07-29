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
	manifestFilename     = "manifest.json"
)

type RegionManifest struct {
	Name         string   `json:"name"`
	SourceSHA256 string   `json:"source_sha256"`
	Tiles        []TileID `json:"tiles"`
}

type Manifest struct {
	FormatVersion        int              `json:"format_version"`
	PreprocessingVersion int              `json:"preprocessing_version"`
	Generation           string           `json:"generation"`
	Zoom                 int              `json:"zoom"`
	Bounds               Bounds           `json:"bounds"`
	Regions              []RegionManifest `json:"regions"`
	Tiles                []TileID         `json:"tiles"`
	BuiltAt              time.Time        `json:"built_at"`
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
	sortTiles(manifest.Tiles)
	manifest.Regions = append([]RegionManifest(nil), manifest.Regions...)
	for i := range manifest.Regions {
		manifest.Regions[i].SourceSHA256 = strings.ToLower(manifest.Regions[i].SourceSHA256)
		manifest.Regions[i].Tiles = append([]TileID(nil), manifest.Regions[i].Tiles...)
		sortTiles(manifest.Regions[i].Tiles)
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
	n, err := tileCount(manifest.Zoom)
	if err != nil {
		return err
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
		if strings.TrimSpace(region.Name) == "" {
			return fmt.Errorf("manifest contains unnamed region")
		}
		if _, exists := regions[region.Name]; exists {
			return fmt.Errorf("manifest contains duplicate region %q", region.Name)
		}
		regions[region.Name] = struct{}{}
		decodedHash, err := hex.DecodeString(region.SourceSHA256)
		if err != nil || len(decodedHash) != 32 {
			return fmt.Errorf("region %q has invalid source SHA-256", region.Name)
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
	return containsTile(manifest.Tiles, tile)
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
