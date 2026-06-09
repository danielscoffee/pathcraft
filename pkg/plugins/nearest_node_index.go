package plugins

import "math"

type IndexedNode struct {
	ID  int64
	Lat float64
	Lon float64
}

type NearestNodeIndex interface {
	Insert(node IndexedNode)
	Rebuild(nodes []IndexedNode)
	NearestCandidates(lat, lon float64) []int64
}

func NewGridNearestNodeIndex() NearestNodeIndex {
	return &gridNearestNodeIndex{
		cells: make(map[cellKey][]int64),
		nodes: make(map[int64]IndexedNode),
		empty: true,
	}
}

type gridNearestNodeIndex struct {
	cells map[cellKey][]int64
	nodes map[int64]IndexedNode
	minX  int
	maxX  int
	minY  int
	maxY  int
	empty bool
}

type cellKey struct {
	x int
	y int
}

const cellSizeDegrees = 0.002

func (s *gridNearestNodeIndex) Insert(node IndexedNode) {
	key := cellFor(node.Lat, node.Lon)
	s.cells[key] = append(s.cells[key], node.ID)
	s.nodes[node.ID] = node

	if s.empty {
		s.minX, s.maxX = key.x, key.x
		s.minY, s.maxY = key.y, key.y
		s.empty = false
		return
	}

	if key.x < s.minX {
		s.minX = key.x
	}
	if key.x > s.maxX {
		s.maxX = key.x
	}
	if key.y < s.minY {
		s.minY = key.y
	}
	if key.y > s.maxY {
		s.maxY = key.y
	}
}

func (s *gridNearestNodeIndex) Rebuild(nodes []IndexedNode) {
	s.cells = make(map[cellKey][]int64)
	s.nodes = make(map[int64]IndexedNode)
	s.empty = true

	for _, node := range nodes {
		s.Insert(node)
	}
}

func (s *gridNearestNodeIndex) NearestCandidates(lat, lon float64) []int64 {
	if s == nil || s.empty {
		return nil
	}

	origin := cellFor(lat, lon)
	maxRadius := maxInt(
		maxInt(absInt(origin.x-s.minX), absInt(origin.x-s.maxX)),
		maxInt(absInt(origin.y-s.minY), absInt(origin.y-s.maxY)),
	)

	candidates := make([]int64, 0)
	bestDistanceSquared := math.Inf(1)
	for radius := 0; radius <= maxRadius; radius++ {
		xMin := origin.x - radius
		xMax := origin.x + radius
		yMin := origin.y - radius
		yMax := origin.y + radius

		for x := xMin; x <= xMax; x++ {
			for y := yMin; y <= yMax; y++ {
				if radius > 0 && x > xMin && x < xMax && y > yMin && y < yMax {
					continue
				}
				for _, id := range s.cells[cellKey{x: x, y: y}] {
					candidates = append(candidates, id)
					if node, ok := s.nodes[id]; ok {
						if d := squaredDegrees(lat, lon, node.Lat, node.Lon); d < bestDistanceSquared {
							bestDistanceSquared = d
						}
					}
				}
			}
		}

		if len(candidates) > 0 && minDistanceToOutsideRadiusSquared(lat, lon, origin, radius) >= bestDistanceSquared {
			return candidates
		}
	}

	return candidates
}

func cellFor(lat, lon float64) cellKey {
	return cellKey{
		x: int(math.Floor(lat / cellSizeDegrees)),
		y: int(math.Floor(lon / cellSizeDegrees)),
	}
}

func squaredDegrees(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := lat2 - lat1
	dLon := lon2 - lon1
	return dLat*dLat + dLon*dLon
}

func minDistanceToOutsideRadiusSquared(lat, lon float64, origin cellKey, radius int) float64 {
	minLat := float64(origin.x-radius) * cellSizeDegrees
	maxLat := float64(origin.x+radius+1) * cellSizeDegrees
	minLon := float64(origin.y-radius) * cellSizeDegrees
	maxLon := float64(origin.y+radius+1) * cellSizeDegrees

	latGap := math.Min(math.Abs(lat-minLat), math.Abs(maxLat-lat))
	lonGap := math.Min(math.Abs(lon-minLon), math.Abs(maxLon-lon))
	gap := math.Min(latGap, lonGap)
	if gap < 0 {
		return 0
	}
	return gap * gap
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
