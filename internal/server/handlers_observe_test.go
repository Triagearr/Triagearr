package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func doObserve(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	r.Header.Set("X-API-Key", testAPIKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TestObserveReadEndpoints smoke-tests the read-only dashboard endpoints
// against a seeded store (one torrent + score + disk usage). They share helper
// plumbing (sinceParam, writeInternal, arrBaseURL) so one pass over each pins
// the happy path for the whole observe surface.
func TestObserveReadEndpoints(t *testing.T) {
	_, _, h := buildSrv(t, "")

	for _, path := range []string{
		"/api/v1/scores",
		"/api/v1/volume",
		"/api/v1/volume/history",
		"/api/v1/volume/history?hours=48",
		"/api/v1/arrs",
		"/api/v1/torrents/categories",
		"/api/v1/torrents/h1/snapshots",
	} {
		t.Run(path, func(t *testing.T) {
			w := doObserve(t, h, path)
			require.Equal(t, http.StatusOK, w.Code)
			require.NotEmpty(t, w.Body.Bytes(), "endpoint returns a JSON body")
		})
	}
}

func TestHealthz(t *testing.T) {
	_, _, h := buildSrv(t, "")
	w := doObserve(t, h, "/healthz")
	require.Equal(t, http.StatusOK, w.Code)
}
