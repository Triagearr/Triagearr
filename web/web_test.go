package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mainBundlePath finds the hashed main bundle the embedded index.html points
// at, so tests assert against a real asset rather than a guessed filename.
func mainBundlePath(t *testing.T) string {
	t.Helper()
	h := Handler()
	if h == nil {
		t.Skip("no dist build embedded; run `bun run build` in web/")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	body := rec.Body.String()
	i := strings.Index(body, "/assets/index-")
	if i < 0 {
		t.Fatalf("index.html has no main bundle reference:\n%s", body)
	}
	return body[i : strings.IndexByte(body[i:], '"')+i]
}

func TestServesGzipWhenAccepted(t *testing.T) {
	h := Handler()
	asset := mainBundlePath(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, asset, nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want to contain Accept-Encoding", got)
	}
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("response is not valid gzip: %v", err)
	}
	if _, err := io.ReadAll(zr); err != nil {
		t.Fatalf("gzip body did not decode: %v", err)
	}
}

func TestServesIdentityWhenGzipNotAccepted(t *testing.T) {
	h := Handler()
	asset := mainBundlePath(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, asset, nil)
	// No Accept-Encoding header at all.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty (identity)", got)
	}
	// Body must be the raw bundle, parseable without inflation.
	if bytes.HasPrefix(rec.Body.Bytes(), []byte{0x1f, 0x8b}) {
		t.Fatal("served gzip magic bytes to a client that didn't ask for gzip")
	}
}

func TestHashedAssetsAreImmutable(t *testing.T) {
	h := Handler()
	asset := mainBundlePath(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, asset, nil))
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("Cache-Control for hashed asset = %q, want immutable", got)
	}

	// index.html must NOT be immutable — it names the hashed bundles.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control for index.html = %q, want no-cache", got)
	}
}

func TestSPAFallbackServesIndex(t *testing.T) {
	h := Handler()
	// A client-side route with no matching file must return the SPA shell.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/settings/scoring", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatal("fallback body is not the SPA shell")
	}
}
