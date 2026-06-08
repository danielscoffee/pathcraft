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
		empty: true,
	}
}

type gridNearestNodeIndex struct {
	cells map[cellKey][]int64
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
	for radius := 0; radius <= maxRadius; radius++ {
		if radius == 0 {
			if ids := s.cells[origin]; len(ids) > 0 {
				return append(candidates, ids...)
			}
			continue
		}

		xMin := origin.x - radius
		xMax := origin.x + radius
		yMin := origin.y - radius
		yMax := origin.y + radius

		for x := xMin; x <= xMax; x++ {
			if ids := s.cells[cellKey{x: x, y: yMin}]; len(ids) > 0 {
				candidates = append(candidates, ids...)
			}
			if yMax != yMin {
				if ids := s.cells[cellKey{x: x, y: yMax}]; len(ids) > 0 {
					candidates = append(candidates, ids...)
				}
			}
		}

		for y := yMin + 1; y < yMax; y++ {
			if ids := s.cells[cellKey{x: xMin, y: y}]; len(ids) > 0 {
				candidates = append(candidates, ids...)
			}
			if xMax != xMin {
				if ids := s.cells[cellKey{x: xMax, y: y}]; len(ids) > 0 {
					candidates = append(candidates, ids...)
				}
			}
		}

		if len(candidates) > 0 {
			return candidates
		}
	}

	return nil
}

func cellFor(lat, lon float64) cellKey {
	return cellKey{
		x: int(math.Floor(lat / cellSizeDegrees)),
		y: int(math.Floor(lon / cellSizeDegrees)),
	}
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
