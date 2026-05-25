package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/server"
	"github.com/Triagearr/Triagearr/internal/store"
)

func buildArrConnSrv(t *testing.T) (http.Handler, *store.Store, *bool) {
	t.Helper()
	s := testStore(t)
	var reloadCalled bool
	srv := server.New(server.Options{
		Bind:    "127.0.0.1:0",
		APIKey:  testAPIKey,
		Store:   s,
		Config:  &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}},
		Version: server.VersionInfo{Version: "test"},
		Reload:  func() { reloadCalled = true },
	})
	return srv.Handler(), s, &reloadCalled
}

func doArrConn(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequestWithContext(t.Context(), method, path, nil)
	} else {
		r = httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("X-API-Key", testAPIKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// upsertBody is a valid input for PUT /api/v1/arr-connections/{kind}.
const upsertSonarrBody = `{"url":"http://sonarr:8989","api_key":"k1","enabled":true,"poll":true,"act":false,"timeout_seconds":30}`

func TestArrConnections_UpsertListDelete(t *testing.T) {
	h, _, reloadCalled := buildArrConnSrv(t)

	// Create via PUT {kind}.
	w := doArrConn(t, h, http.MethodPut, "/api/v1/arr-connections/sonarr", upsertSonarrBody)
	require.Equal(t, http.StatusOK, w.Code)
	var saved struct {
		Kind string `json:"kind"`
		Act  bool   `json:"act"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&saved))
	require.Equal(t, "sonarr", saved.Kind)
	require.True(t, *reloadCalled)

	// List.
	w = doArrConn(t, h, http.MethodGet, "/api/v1/arr-connections", "")
	require.Equal(t, http.StatusOK, w.Code)
	var list struct {
		Connections []struct {
			Kind string `json:"kind"`
			URL  string `json:"url"`
		} `json:"connections"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Len(t, list.Connections, 1)
	require.Equal(t, "sonarr", list.Connections[0].Kind)

	// Update (upsert with new URL).
	update := `{"url":"http://sonarr:9999","api_key":"k1","enabled":true,"poll":true,"act":true,"timeout_seconds":45}`
	w = doArrConn(t, h, http.MethodPut, "/api/v1/arr-connections/sonarr", update)
	require.Equal(t, http.StatusOK, w.Code)

	w = doArrConn(t, h, http.MethodGet, "/api/v1/arr-connections", "")
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Equal(t, "http://sonarr:9999", list.Connections[0].URL)

	// Delete.
	w = doArrConn(t, h, http.MethodDelete, "/api/v1/arr-connections/sonarr", "")
	require.Equal(t, http.StatusNoContent, w.Code)

	w = doArrConn(t, h, http.MethodGet, "/api/v1/arr-connections", "")
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Empty(t, list.Connections)
}

func TestArrConnections_UpsertIsIdempotent(t *testing.T) {
	h, _, _ := buildArrConnSrv(t)
	// Two PUTs for the same kind must succeed (upsert semantics).
	require.Equal(t, http.StatusOK, doArrConn(t, h, http.MethodPut, "/api/v1/arr-connections/sonarr", upsertSonarrBody).Code)
	require.Equal(t, http.StatusOK, doArrConn(t, h, http.MethodPut, "/api/v1/arr-connections/sonarr", upsertSonarrBody).Code)
}

func TestArrConnections_RejectsInvalidInput(t *testing.T) {
	h, _, _ := buildArrConnSrv(t)

	// Unknown kind → 400.
	require.Equal(t, http.StatusBadRequest, doArrConn(t, h, http.MethodPut, "/api/v1/arr-connections/plex", upsertSonarrBody).Code)

	// Enabled but no api_key → 400.
	noKey := `{"url":"http://sonarr:8989","api_key":"","enabled":true}`
	require.Equal(t, http.StatusBadRequest, doArrConn(t, h, http.MethodPut, "/api/v1/arr-connections/sonarr", noKey).Code)
}

func TestArrConnections_DeleteUnknownKind(t *testing.T) {
	h, _, _ := buildArrConnSrv(t)
	// Deleting a kind that was never inserted → 404.
	require.Equal(t, http.StatusNotFound, doArrConn(t, h, http.MethodDelete, "/api/v1/arr-connections/radarr", "").Code)
}
