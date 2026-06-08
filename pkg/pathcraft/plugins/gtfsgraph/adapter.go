// Package gtfsgraph adapts an internal GTFS StopTimeIndex (with optional
// transfers) to the public core.Graph interface and exposes a Native
// capability for transit algorithms like RAPTOR.
package gtfsgraph

import (
	"context"

	"github.com/danielscoffee/pathcraft/internal/gtfs"
	"github.com/danielscoffee/pathcraft/internal/routing/raptor"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
)

// Native is the escape hatch used by the raptor plugin.
type Native interface {
	NativeIndex() *gtfs.StopTimeIndex
	NativeTransfers() map[gtfs.StopID][]raptor.Transfer
	NativeStops() map[gtfs.StopID]gtfs.Stop
}

// Wrapper implements core.Graph (with empty Neighbors — transit graphs are
// time-dependent and not edge-relaxation friendly) plus core.Coordinated
// and Native.
type Wrapper struct {
	Index     *gtfs.StopTimeIndex
	Transfers map[gtfs.StopID][]raptor.Transfer
	Stops     map[gtfs.StopID]gtfs.Stop
}

func (w *Wrapper) NativeIndex() *gtfs.StopTimeIndex                   { return w.Index }
func (w *Wrapper) NativeTransfers() map[gtfs.StopID][]raptor.Transfer { return w.Transfers }
func (w *Wrapper) NativeStops() map[gtfs.StopID]gtfs.Stop             { return w.Stops }

func (w *Wrapper) Neighbors(_ context.Context, _ core.NodeID) ([]core.Edge, error) {
	// Transit graphs do not expose static edges; transit algorithms use the
	// Native capability instead.
	return nil, nil
}

func (w *Wrapper) Coord(id core.NodeID) (float64, float64, bool) {
	stop, ok := w.Stops[gtfs.StopID(id)]
	if !ok {
		return 0, 0, false
	}
	return stop.Lat, stop.Lon, true
}
