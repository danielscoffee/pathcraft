package worldgraph

import (
	"math"
	"reflect"
	"testing"
)

func TestTileForPosition(t *testing.T) {
	tile, err := TileForPosition(0, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := (TileID{Z: 1, X: 1, Y: 1}); tile != want {
		t.Fatalf("TileForPosition() = %+v, want %+v", tile, want)
	}

	wrapped, err := TileForPosition(190, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := TileForPosition(-170, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if wrapped != canonical {
		t.Fatalf("wrapped longitude tile = %+v, want %+v", wrapped, canonical)
	}
}

func TestTileForPositionRejectsMercatorOverflow(t *testing.T) {
	tests := []struct {
		name string
		lon  float64
		lat  float64
	}{
		{name: "north", lat: MaxMercatorLatitude + 0.000001},
		{name: "south", lat: -MaxMercatorLatitude - 0.000001},
		{name: "NaN longitude", lon: math.NaN()},
		{name: "infinite longitude", lon: math.Inf(1)},
		{name: "NaN latitude", lat: math.NaN()},
		{name: "infinite latitude", lat: math.Inf(-1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := TileForPosition(test.lon, test.lat, DefaultZoom); err == nil {
				t.Fatal("TileForPosition() error = nil, want rejection")
			}
		})
	}
}

func TestTileBoundsRoundTrip(t *testing.T) {
	for _, id := range []TileID{
		{Z: 0, X: 0, Y: 0},
		{Z: 4, X: 3, Y: 6},
		{Z: 4, X: 15, Y: 15},
	} {
		bounds := id.Bounds()
		if !(bounds.West < bounds.East && bounds.South < bounds.North) {
			t.Fatalf("%+v.Bounds() = %+v, want ordered bounds", id, bounds)
		}
		got, err := TileForPosition(
			(bounds.West+bounds.East)/2,
			(bounds.South+bounds.North)/2,
			id.Z,
		)
		if err != nil {
			t.Fatal(err)
		}
		if got != id {
			t.Fatalf("bounds center maps to %+v, want %+v", got, id)
		}
	}
}

func TestWrappedCorridorUsesShortestAntimeridianSpan(t *testing.T) {
	got, err := Corridor(
		TileID{Z: 3, X: 7, Y: 3},
		TileID{Z: 3, X: 0, Y: 4},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []TileID{
		{Z: 3, X: 0, Y: 3},
		{Z: 3, X: 0, Y: 4},
		{Z: 3, X: 7, Y: 3},
		{Z: 3, X: 7, Y: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Corridor() = %+v, want %+v", got, want)
	}
}

func TestTileRingClampsYAndWrapsX(t *testing.T) {
	got, err := Expand([]TileID{{Z: 2, X: 0, Y: 0}}, 1, 6)
	if err != nil {
		t.Fatal(err)
	}
	want := []TileID{
		{Z: 2, X: 0, Y: 0},
		{Z: 2, X: 0, Y: 1},
		{Z: 2, X: 1, Y: 0},
		{Z: 2, X: 1, Y: 1},
		{Z: 2, X: 3, Y: 0},
		{Z: 2, X: 3, Y: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Expand() = %+v, want %+v", got, want)
	}
	if _, err := Expand([]TileID{{Z: 2, X: 0, Y: 0}}, 1, 5); err == nil {
		t.Fatal("Expand() error = nil, want maxTiles rejection")
	}
}
