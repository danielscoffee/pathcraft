// Package web embeds the built single-page application (web/app/dist) so the
// HTTP server can serve the frontend from a single binary. Build the SPA with
// `make web` (or `npm run build` inside web/app) before `go build`.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:app/dist
var dist embed.FS

// Dist returns the SPA build output rooted at the dist directory. When the
// frontend has not been built, the returned filesystem contains no index.html
// and callers should degrade gracefully.
func Dist() (fs.FS, error) {
	return fs.Sub(dist, "app/dist")
}
