// Ref: .teos/sections/10, .teos/sections/12 — single-binary asset serving.
// go:embed of the built SPA; SPA-fallback to index.html for client routes.
package server

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:web
var webFS embed.FS

// staticHandler serves the embedded SvelteKit build with SPA fallback.
func (s *Server) staticHandler() http.HandlerFunc {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		sub = webFS
	}
	fileServer := http.FileServer(http.FS(sub))
	return func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean("/" + r.URL.Path)
		if f, err := sub.Open(strings.TrimPrefix(p, "/")); err == nil {
			f.Close()
			// Vite emits content-hashed names under _app/immutable —
			// safe to cache effectively forever. Everything else (the
			// index.html entry point above all) revalidates each load —
			// heuristic caching of it is how stale bundles linger.
			if strings.HasPrefix(p, "/_app/immutable/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback — client-side routing owns unknown paths.
		w.Header().Set("Cache-Control", "no-cache")
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}
}
