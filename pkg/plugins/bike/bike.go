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
		Options: []core.ModeOption{
			{Name: "speed_mps", Label: "Speed", Kind: "number", Default: "4.5"},
		},
	}
}

func (plugin Plugin) Route(ctx context.Context, host any, req core.ModeRequest) (core.ModeResult, error) {
	speed, err := modeutil.PositiveOption(req.Options, "speed_mps", speedMPS)
	if err != nil {
		return core.ModeResult{}, err
	}
	return modeutil.StreetRoute(ctx, host, req, plugin.Manifest(), mobility.NewWalking(speed))
}

func init() { plugins.MustRegisterMode(Plugin{}) }
