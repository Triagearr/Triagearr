// Package web embeds the Vite-built React UI so the daemon ships as a
// single binary. The contents of dist/ are baked in at compile time via
// //go:embed; running `bun run build` inside web/ before `go build` refreshes
// them.
//
// The Handler() helper resolves requests against dist/, falling back to
// index.html for any non-asset path so TanStack Router's in-memory routes
// keep working on full-page reloads.
//
// Assets are precompressed with gzip once at startup (Vite emits no
// Content-Encoding of its own, and net/http's FileServer doesn't compress).
// The embedded bundle is large and immutable, so paying the compression cost
// once at boot — rather than per request, or not at all — keeps transfer size
// ~3x smaller for every page load without burning CPU on a hot path.
package web

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// asset is a single file from dist/, held in memory both raw and gzip-encoded
// so the handler can negotiate Content-Encoding without touching disk or
// re-compressing.
type asset struct {
	raw         []byte
	gz          []byte // nil when compression didn't pay off or type isn't compressible
	contentType string
	immutable   bool // content-hashed filename → safe to cache forever
}

// compressibleExt is the set of extensions worth gzipping. Already-compressed
// formats (woff2, png, …) gain nothing and would just waste boot time.
var compressibleExt = map[string]bool{
	".js":   true,
	".css":  true,
	".html": true,
	".svg":  true,
	".json": true,
	".txt":  true,
	".map":  true,
	".ico":  true,
}

// Handler returns the SPA handler. Returns nil if dist/index.html is missing
// (Vite hasn't produced a build yet); callers should treat that as "UI
// disabled" rather than panic.
func Handler() http.Handler {
	root, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	index, err := loadAsset(root, "index.html")
	if err != nil {
		return nil
	}

	assets := map[string]*asset{"index.html": index}
	_ = fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == "index.html" {
			return nil
		}
		a, err := loadAsset(root, p)
		if err != nil {
			return nil
		}
		assets[p] = a
		return nil
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		// Anything without a file extension is an SPA route (or "/"): hand it
		// index.html and let TanStack Router resolve it client-side. Hashed
		// asset paths that 404 also fall back, matching the old behaviour.
		a, ok := assets[p]
		if !ok || !hasFileExt(p) {
			a = index
		}
		serveAsset(w, r, a)
	})
}

// loadAsset reads a file from the embedded FS and precomputes its gzip body
// and cache class. Compression is kept only when it actually shrinks the file.
func loadAsset(root fs.FS, p string) (*asset, error) {
	raw, err := fs.ReadFile(root, p)
	if err != nil {
		return nil, err
	}
	ct := mime.TypeByExtension(path.Ext(p))
	if ct == "" {
		ct = "application/octet-stream"
	}
	a := &asset{
		raw:         raw,
		contentType: ct,
		// Vite writes content-hashed names under assets/; those can never
		// change behind a given URL, so they're safe to cache immutably.
		immutable: strings.HasPrefix(p, "assets/"),
	}
	if compressibleExt[strings.ToLower(path.Ext(p))] && len(raw) > 0 {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if _, err := zw.Write(raw); err == nil && zw.Close() == nil && buf.Len() < len(raw) {
			a.gz = append([]byte(nil), buf.Bytes()...)
		}
	}
	return a, nil
}

func serveAsset(w http.ResponseWriter, r *http.Request, a *asset) {
	h := w.Header()
	h.Set("Content-Type", a.contentType)
	if a.immutable {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// index.html references hashed assets by name, so it must always be
		// revalidated or a stale shell would point at deleted bundles.
		h.Set("Cache-Control", "no-cache")
	}
	body := a.raw
	if a.gz != nil {
		h.Add("Vary", "Accept-Encoding")
		if acceptsGzip(r) {
			h.Set("Content-Encoding", "gzip")
			body = a.gz
		}
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(body)
}

func acceptsGzip(r *http.Request) bool {
	for _, enc := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.EqualFold(strings.TrimSpace(strings.SplitN(enc, ";", 2)[0]), "gzip") {
			return true
		}
	}
	return false
}

func hasFileExt(p string) bool {
	return strings.Contains(path.Base(p), ".")
}
