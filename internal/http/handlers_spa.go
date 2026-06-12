package http

import (
	"io/fs"
	"net/http"

	"github.com/danielscoffee/pathcraft/web"
)

// spaHandler serves the embedded React frontend. Unknown paths fall back to
// index.html so client-side routing keeps working. When the frontend bundle
// is absent (web/app/dist not built), it answers 503 with a build hint.
func spaHandler() http.Handler {
	distFS, err := web.Dist()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "frontend unavailable: "+err.Error(), http.StatusServiceUnavailable)
		})
	}

	fileServer := http.FileServer(http.FS(distFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" {
			if _, err := fs.Stat(distFS, path[1:]); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		index, err := fs.ReadFile(distFS, "index.html")
		if err != nil {
			http.Error(w,
				"frontend not built: run `make web` (builds web/app) and rebuild the server",
				http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}
