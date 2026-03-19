package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// uiDist holds the built React app from packages/ui/dist/.
// In CI, the build step copies the Vite output here before `go build`.
// In dev without the UI built, the placeholder index.html is served.
//
//go:embed ui/dist
var uiDist embed.FS

// uiHandler returns an http.Handler that serves the embedded React app at /ui/.
// All paths that don't match a real file fall back to index.html (SPA routing).
func uiHandler() http.Handler {
	// Strip the "ui/dist" prefix from embedded paths so they're accessible at /
	sub, err := fs.Sub(uiDist, "ui/dist")
	if err != nil {
		// Should never happen — directory always exists due to placeholder
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI not available", http.StatusServiceUnavailable)
		})
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip the /ui prefix before serving
		path := strings.TrimPrefix(r.URL.Path, "/ui")
		if path == "" {
			path = "/"
		}

		// Check if the file exists in the embedded FS
		if path != "/" && path != "" {
			f, err := sub.Open(strings.TrimPrefix(path, "/"))
			if err == nil {
				f.Close()
				// File exists — serve it directly
				r.URL.Path = path
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// Fall back to index.html for SPA client-side routing
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		idx, err := uiDist.ReadFile("ui/dist/index.html")
		if err != nil {
			http.Error(w, "UI index not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write(idx)
	})
}
