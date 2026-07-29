package worldgraph

import (
	"fmt"
	"math"
	"sort"
)

const (
	DefaultZoom          = 12
	MaxMercatorLatitude  = 85.05112878
	maxTileZoom          = 30
	defaultRouteMaxTiles = 256
)

type TileID struct {
	Z int
	X int
	Y int
}

type Bounds struct {
	West  float64
	South float64
	East  float64
	North float64
}

func TileForPosition(lon, lat float64, zoom int) (TileID, error) {
	n, err := tileCount(zoom)
	if err != nil {
		return TileID{}, err
	}
	if math.IsNaN(lon) || math.IsInf(lon, 0) || math.IsNaN(lat) || math.IsInf(lat, 0) {
		return TileID{}, fmt.Errorf("position must be finite")
	}
	if lat < -MaxMercatorLatitude || lat > MaxMercatorLatitude {
		return TileID{}, fmt.Errorf("latitude %v outside Web Mercator bounds", lat)
	}

	wrappedLon := math.Mod(lon+180, 360)
	if wrappedLon < 0 {
		wrappedLon += 360
	}
	x := int(math.Floor(wrappedLon / 360 * float64(n)))
	latRad := lat * math.Pi / 180
	y := int(math.Floor((1 - math.Asinh(math.Tan(latRad))/math.Pi) / 2 * float64(n)))
	if y < 0 {
		y = 0
	} else if y >= n {
		y = n - 1
	}
	return TileID{Z: zoom, X: x, Y: y}, nil
}

func (id TileID) Bounds() Bounds {
	n, err := tileCount(id.Z)
	if err != nil || id.X < 0 || id.X >= n || id.Y < 0 || id.Y >= n {
		nan := math.NaN()
		return Bounds{West: nan, South: nan, East: nan, North: nan}
	}

	west := float64(id.X)/float64(n)*360 - 180
	east := float64(id.X+1)/float64(n)*360 - 180
	north := tileLatitude(id.Y, n)
	south := tileLatitude(id.Y+1, n)
	return Bounds{West: west, South: south, East: east, North: north}
}

func Corridor(from, to TileID, halo int) ([]TileID, error) {
	return corridor(from, to, halo, defaultRouteMaxTiles)
}

func corridor(from, to TileID, halo, maxTiles int) ([]TileID, error) {
	n, err := validatePair(from, to)
	if err != nil {
		return nil, err
	}
	if halo < 0 {
		return nil, fmt.Errorf("halo must not be negative")
	}
	if maxTiles <= 0 {
		return nil, fmt.Errorf("maxTiles must be positive")
	}

	halo = min(halo, n)
	minX, maxX := min(from.X, to.X), max(from.X, to.X)
	span := maxX - minX
	wrapped := span > n-span
	xCount := span + 1
	if wrapped {
		xCount = n - span + 1
	}
	xRadius := min(halo, n/2)
	expandedXCount := min(n, xCount+2*xRadius)
	minY := max(0, min(from.Y, to.Y)-halo)
	maxY := min(n-1, max(from.Y, to.Y)+halo)
	candidateCount := int64(expandedXCount) * int64(maxY-minY+1)
	if candidateCount > int64(maxTiles) {
		return nil, fmt.Errorf("tile corridor exceeds limit %d", maxTiles)
	}

	xs := make([]int, 0, xCount)
	if !wrapped {
		for x := minX; x <= maxX; x++ {
			xs = append(xs, x)
		}
	} else {
		for x := maxX; x < n; x++ {
			xs = append(xs, x)
		}
		for x := 0; x <= minX; x++ {
			xs = append(xs, x)
		}
	}

	tiles := make(map[TileID]struct{}, int(candidateCount))
	for _, x := range xs {
		for dx := -xRadius; dx <= xRadius; dx++ {
			wrappedX := wrap(x+dx, n)
			for y := minY; y <= maxY; y++ {
				tiles[TileID{Z: from.Z, X: wrappedX, Y: y}] = struct{}{}
			}
		}
	}
	return sortedTileIDs(tiles), nil
}

func Expand(tiles []TileID, rings, maxTiles int) ([]TileID, error) {
	if rings < 0 {
		return nil, fmt.Errorf("rings must not be negative")
	}
	if len(tiles) == 0 {
		return []TileID{}, nil
	}
	if maxTiles <= 0 {
		return nil, fmt.Errorf("maxTiles must be positive")
	}

	n, err := tileCount(tiles[0].Z)
	if err != nil {
		return nil, err
	}
	type tileStep struct {
		tile TileID
		step int
	}
	result := make(map[TileID]struct{}, min(len(tiles), maxTiles))
	queue := make([]tileStep, 0, min(len(tiles), maxTiles))
	for _, tile := range tiles {
		if tile.Z != tiles[0].Z {
			return nil, fmt.Errorf("tiles must use one zoom")
		}
		if err := validateTile(tile, n); err != nil {
			return nil, err
		}
		if _, exists := result[tile]; exists {
			continue
		}
		if len(result) == maxTiles {
			return nil, fmt.Errorf("tile expansion exceeds limit %d", maxTiles)
		}
		result[tile] = struct{}{}
		queue = append(queue, tileStep{tile: tile})
	}

	for head := 0; head < len(queue); head++ {
		current := queue[head]
		if current.step >= rings {
			continue
		}
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				if dx == 0 && dy == 0 {
					continue
				}
				y := current.tile.Y + dy
				if y < 0 || y >= n {
					continue
				}
				neighbor := TileID{Z: current.tile.Z, X: wrap(current.tile.X+dx, n), Y: y}
				if _, exists := result[neighbor]; exists {
					continue
				}
				if len(result) == maxTiles {
					return nil, fmt.Errorf("tile expansion exceeds limit %d", maxTiles)
				}
				result[neighbor] = struct{}{}
				queue = append(queue, tileStep{tile: neighbor, step: current.step + 1})
			}
		}
	}
	return sortedTileIDs(result), nil
}

func tileCount(zoom int) (int, error) {
	if zoom < 0 || zoom > maxTileZoom {
		return 0, fmt.Errorf("zoom %d outside supported range 0..%d", zoom, maxTileZoom)
	}
	return 1 << zoom, nil
}

func validatePair(from, to TileID) (int, error) {
	if from.Z != to.Z {
		return 0, fmt.Errorf("tiles must use one zoom")
	}
	n, err := tileCount(from.Z)
	if err != nil {
		return 0, err
	}
	if err := validateTile(from, n); err != nil {
		return 0, err
	}
	if err := validateTile(to, n); err != nil {
		return 0, err
	}
	return n, nil
}

func validateTile(id TileID, n int) error {
	if id.X < 0 || id.X >= n || id.Y < 0 || id.Y >= n {
		return fmt.Errorf("invalid tile %+v", id)
	}
	return nil
}

func tileLatitude(y, n int) float64 {
	return math.Atan(math.Sinh(math.Pi*(1-2*float64(y)/float64(n)))) * 180 / math.Pi
}

func wrap(value, n int) int {
	value %= n
	if value < 0 {
		value += n
	}
	return value
}

func sortedTileIDs(set map[TileID]struct{}) []TileID {
	tiles := make([]TileID, 0, len(set))
	for tile := range set {
		tiles = append(tiles, tile)
	}
	sort.Slice(tiles, func(i, j int) bool {
		if tiles[i].Z != tiles[j].Z {
			return tiles[i].Z < tiles[j].Z
		}
		if tiles[i].X != tiles[j].X {
			return tiles[i].X < tiles[j].X
		}
		return tiles[i].Y < tiles[j].Y
	})
	return tiles
}
