package air

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/danielscoffee/pathcraft/internal/geo"
	"github.com/danielscoffee/pathcraft/pkg/pathcraft/core"
	"github.com/danielscoffee/pathcraft/pkg/plugins"
	"github.com/danielscoffee/pathcraft/pkg/plugins/internal/modeutil"
)

const (
	defaultCruiseAltitudeM = 1000.0
	defaultSpeedMPS        = 230.0
)

type Plugin struct{}

func (Plugin) Name() string { return "air" }

func (Plugin) Manifest() core.ModeManifest {
	return core.ModeManifest{
		ID:         "air",
		Label:      "Air",
		Icon:       "air",
		Color:      "#7c3aed",
		CRS:        "EPSG:4326",
		Dimensions: []int{2, 3},
		Axes:       []string{"longitude", "latitude", "altitude_m"},
		Options: []core.ModeOption{
			{Name: "cruise_altitude_m", Label: "Cruise altitude", Kind: "number", Default: "1000"},
			{Name: "speed_mps", Label: "Speed", Kind: "number", Default: "230"},
		},
	}
}

func (Plugin) Route(ctx context.Context, _ any, req core.ModeRequest) (core.ModeResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ModeResult{}, err
	}
	if len(req.From) < 2 || len(req.From) > 3 || len(req.To) < 2 || len(req.To) > 3 {
		return core.ModeResult{}, fmt.Errorf("air mode requires 2D or 3D positions")
	}
	fromLon, fromLat, err := modeutil.GeographicPosition("from", req.From)
	if err != nil {
		return core.ModeResult{}, err
	}
	toLon, toLat, err := modeutil.GeographicPosition("to", req.To)
	if err != nil {
		return core.ModeResult{}, err
	}
	fromAltitude, err := altitude("from", req.From)
	if err != nil {
		return core.ModeResult{}, err
	}
	toAltitude, err := altitude("to", req.To)
	if err != nil {
		return core.ModeResult{}, err
	}
	cruiseAltitude, err := positiveOption(req.Options, "cruise_altitude_m", defaultCruiseAltitudeM)
	if err != nil {
		return core.ModeResult{}, err
	}
	speed, err := positiveOption(req.Options, "speed_mps", defaultSpeedMPS)
	if err != nil {
		return core.ModeResult{}, err
	}
	cruiseAltitude = max(cruiseAltitude, fromAltitude, toAltitude)

	groundDistance := geo.HaversineDistance(fromLat, fromLon, toLat, toLon)
	halfGround := groundDistance / 2
	distance := math.Hypot(halfGround, cruiseAltitude-fromAltitude) +
		math.Hypot(halfGround, cruiseAltitude-toAltitude)
	duration := int64(math.Ceil(distance / speed))
	positions := []core.Position{
		{fromLon, fromLat, fromAltitude},
		{(fromLon + toLon) / 2, (fromLat + toLat) / 2, cruiseAltitude},
		{toLon, toLat, toAltitude},
	}

	return core.ModeResult{
		Mode:            "air",
		DurationSeconds: duration,
		DistanceMeters:  distance,
		Segments: []core.RouteSegment{{
			Kind:            "air",
			Label:           "Direct flight",
			Color:           "#7c3aed",
			Dashed:          true,
			Positions:       positions,
			DistanceMeters:  distance,
			DurationSeconds: duration,
		}},
		Meta: map[string]any{
			"cruise_altitude_m": cruiseAltitude,
			"speed_mps":         speed,
		},
	}, nil
}

func altitude(name string, position core.Position) (float64, error) {
	if len(position) == 2 {
		return 0, nil
	}
	value := position[2]
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s altitude must be finite", name)
	}
	return value, nil
}

func positiveOption(options map[string]string, name string, fallback float64) (float64, error) {
	value := options[name]
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("%s must be a positive finite number", name)
	}
	return parsed, nil
}

func init() { plugins.MustRegisterMode(Plugin{}) }
