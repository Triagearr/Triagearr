// Package web embeds the Vite-built React UI so the daemon ships as a
// single binary. The contents of dist/ are baked in at compile time via
// //go:embed; running `bun run build` inside web/ before `go build` refreshes
// them.
//
// The Handler() helper resolves requests against dist/, falling back to
// index.html for any non-asset path so TanStack Router's in-memory routes
// keep working on full-page reloads.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns the SPA handler. Returns nil if dist/index.html is missing
// (Vite hasn't produced a build yet); callers should treat that as "UI
// disabled" rather than panic.
func Handler() http.Handler {
	root, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(root, "index.html"); err != nil {
		return nil
	}
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			fileServer.ServeHTTP(w, r)
			return
		}
		// Asset paths (anything with a dot in the last segment) get served
		// straight or 404. Everything else falls back to index.html so the
		// SPA router takes over.
		if !hasFileExt(p) {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		if _, err := fs.Stat(root, p); err != nil {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func hasFileExt(p string) bool {
	last := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		last = p[i+1:]
	}
	return strings.Contains(last, ".")
}
