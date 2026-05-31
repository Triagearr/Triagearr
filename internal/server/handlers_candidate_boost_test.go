package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/config"
)

func TestCandidateBoostEndpoint_ToggleSurfacesInGet(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	// GET pre-state: not boosted.
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/torrents/h1", nil)
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, false, got["candidate_boost"])

	// PUT boost.
	w = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(context.Background(), http.MethodPut,
		"/api/v1/torrents/h1/candidate_boost", strings.NewReader(`{"candidate_boost":true}`))
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	// GET reflects the flag.
	w = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/torrents/h1", nil)
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, true, got["candidate_boost"])
	require.NotEmpty(t, got["candidate_boost_at"], "candidate_boost_at must be set when boost=true")

	// PUT un-boost.
	w = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(context.Background(), http.MethodPut,
		"/api/v1/torrents/h1/candidate_boost", strings.NewReader(`{"candidate_boost":false}`))
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/torrents/h1", nil)
	h.ServeHTTP(w, req)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, false, got["candidate_boost"])
}

// Boosting must clear an existing protect (the two are mutually exclusive,
// ADR-0030), surfaced end-to-end through the GET response.
func TestCandidateBoostEndpoint_ClearsProtect(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	put := func(path, body string) {
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, path, strings.NewReader(body))
		h.ServeHTTP(w, req)
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	}

	put("/api/v1/torrents/h1/protected", `{"protected":true}`)
	put("/api/v1/torrents/h1/candidate_boost", `{"candidate_boost":true}`)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/torrents/h1", nil)
	h.ServeHTTP(w, req)
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, true, got["candidate_boost"])
	require.Equal(t, false, got["protected"], "boosting must clear the protect flag")
}

func TestCandidateBoostEndpoint_UnknownHash_404(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut,
		"/api/v1/torrents/doesnotexist/candidate_boost", strings.NewReader(`{"candidate_boost":true}`))
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
