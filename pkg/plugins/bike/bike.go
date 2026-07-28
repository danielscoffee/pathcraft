package bike

import (
	"context"

	"github.com/danielscoffee/pathcraft/internal/mobility"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
	"github.com/danielscoffee/pathcraft/pkg/plugins/internal/modeutil"
)

const speedMPS = 4.5

type Plugin struct{}

func (Plugin) Name() string { return "bike" }

func (Plugin) Manifest() core.ModeManifest {
	return core.ModeManifest{
		ID:         "bike",
		Label:      "Bike",
		Icon:       "bike",
		Color:      "#2f7d4f",
		CRS:        "EPSG:4326",
		Dimensions: []int{2},
		Axes:       []string{"longitude", "latitude"},
	}
}

func (plugin Plugin) Route(ctx context.Context, host any, req core.ModeRequest) (core.ModeResult, error) {
	return modeutil.StreetRoute(ctx, host, req, plugin.Manifest(), mobility.NewWalking(speedMPS))
}

func init() { plugins.MustRegisterMode(Plugin{}) }
