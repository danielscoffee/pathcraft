package core

import "context"

// Exporter serializes a RouteResult into bytes (GeoJSON, JSON, HTML, ...).
// It receives the source Graph so it can resolve node coordinates when needed.
type Exporter interface {
	Name() string
	MimeType() string
	Export(ctx context.Context, result RouteResult, g Graph) ([]byte, error)
}
