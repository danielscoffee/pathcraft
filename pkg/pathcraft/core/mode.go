package core

import "context"

// Position is an ordered, plugin-defined N-dimensional coordinate.
type Position []float64

// ModeOption describes one optional routing input exposed to adapters.
type ModeOption struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	Default  string `json:"default,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// ModeManifest lets adapters discover a routing mode without knowing its implementation.
type ModeManifest struct {
	ID         string       `json:"id"`
	Label      string       `json:"label"`
	Icon       string       `json:"icon,omitempty"`
	Color      string       `json:"color,omitempty"`
	CRS        string       `json:"crs,omitempty"`
	Dimensions []int        `json:"dimensions,omitempty"`
	Axes       []string     `json:"axes,omitempty"`
	Options    []ModeOption `json:"options,omitempty"`
}

// ModeRequest is transport-neutral. Position semantics belong to the plugin manifest.
type ModeRequest struct {
	From    Position          `json:"from"`
	To      Position          `json:"to"`
	Options map[string]string `json:"options,omitempty"`
}

// RouteSegment is one styled, N-dimensional section of a route.
type RouteSegment struct {
	Kind            string         `json:"kind,omitempty"`
	Label           string         `json:"label,omitempty"`
	Color           string         `json:"color,omitempty"`
	Dashed          bool           `json:"dashed,omitempty"`
	Positions       []Position     `json:"positions"`
	DistanceMeters  float64        `json:"distance_meters,omitempty"`
	DurationSeconds int64          `json:"duration_seconds,omitempty"`
	Meta            map[string]any `json:"meta,omitempty"`
}

// ModeResult is the common result consumed by transport and visual adapters.
type ModeResult struct {
	Mode            string         `json:"mode"`
	DurationSeconds int64          `json:"duration_seconds,omitempty"`
	DistanceMeters  float64        `json:"distance_meters,omitempty"`
	Segments        []RouteSegment `json:"segments"`
	Meta            map[string]any `json:"meta,omitempty"`
}

// Mode is a high-level routing plugin. Host is intentionally opaque: core
// cannot predict which runtime capabilities future land, sea, air, indoor, or
// orbital plugins require.
type Mode interface {
	Name() string
	Manifest() ModeManifest
	Route(ctx context.Context, host any, req ModeRequest) (ModeResult, error)
}
