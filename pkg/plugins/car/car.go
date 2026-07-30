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
		Options: []core.ModeOption{
			{Name: "speed_mps", Label: "Speed", Kind: "number", Default: "13.9"},
		},
	}
}

func (plugin Plugin) Route(ctx context.Context, host any, req core.ModeRequest) (core.ModeResult, error) {
	speed, err := modeutil.PositiveOption(req.Options, "speed_mps", mobility.DefaultDrivingSpeedMPS)
	if err != nil {
		return core.ModeResult{}, err
	}
	return modeutil.StreetRoute(ctx, host, req, plugin.Manifest(), mobility.NewDriving(speed))
}

func init() { plugins.MustRegisterMode(Plugin{}) }
