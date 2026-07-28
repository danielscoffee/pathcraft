package car

import (
	"context"

	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
	"github.com/danielscoffee/pathcraft/pkg/plugins/internal/modeutil"
)

type Plugin struct{}

func (Plugin) Name() string { return "car" }

func (Plugin) Manifest() core.ModeManifest {
	return core.ModeManifest{
		ID:         "car",
		Label:      "Car",
		Icon:       "car",
		Color:      "#b45309",
		CRS:        "EPSG:4326",
		Dimensions: []int{2},
		Axes:       []string{"longitude", "latitude"},
	}
}

func (plugin Plugin) Route(ctx context.Context, host any, req core.ModeRequest) (core.ModeResult, error) {
	return modeutil.StreetRoute(ctx, host, req, plugin.Manifest(), mobility.NewDriving(mobility.DefaultDrivingSpeedMPS))
}

func init() { plugins.MustRegisterMode(Plugin{}) }
