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

func TestProtectEndpoint_ToggleSurfacesInGet(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	// GET pre-state: not protected.
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/torrents/h1", nil)
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, false, got["protected"])

	// PUT protect.
	w = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(context.Background(), http.MethodPut,
		"/api/v1/torrents/h1/protected", strings.NewReader(`{"protected":true}`))
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	// GET reflects the flag.
	w = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/torrents/h1", nil)
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, true, got["protected"])
	require.NotEmpty(t, got["protected_at"], "protected_at must be set when protect=true")

	// PUT unprotect.
	w = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(context.Background(), http.MethodPut,
		"/api/v1/torrents/h1/protected", strings.NewReader(`{"protected":false}`))
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/torrents/h1", nil)
	h.ServeHTTP(w, req)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, false, got["protected"])
}

func TestProtectEndpoint_UnknownHash_404(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut,
		"/api/v1/torrents/doesnotexist/protected", strings.NewReader(`{"protected":true}`))
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
