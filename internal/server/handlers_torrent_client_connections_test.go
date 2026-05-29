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

func buildTorrentConnSrv(t *testing.T) (http.Handler, *bool) {
	t.Helper()
	s := testStore(t)
	var reloadCalled bool
	srv := server.New(server.Options{
		Bind:    "127.0.0.1:0",
		APIKey:  testAPIKey,
		Store:   s,
		Config:  &config.Config{},
		Version: server.VersionInfo{Version: "test"},
		Reload:  func() { reloadCalled = true },
	})
	return srv.Handler(), &reloadCalled
}

func doTorrentConn(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
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

const qbitConnBody = `{"url":"http://qbit:8080/","username":"admin","password":"pw","enabled":true,"delete_with_files":true,"timeout_seconds":20}`

func TestTorrentClientConnections_UpsertListDelete(t *testing.T) {
	h, reloadCalled := buildTorrentConnSrv(t)

	// Empty list initially.
	w := doTorrentConn(t, h, http.MethodGet, "/api/v1/torrent-client-connections", "")
	require.Equal(t, http.StatusOK, w.Code)
	var listResp struct {
		Connections []store.TorrentClientConnection `json:"connections"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&listResp))
	require.Empty(t, listResp.Connections)

	// Create qbittorrent.
	w = doTorrentConn(t, h, http.MethodPut, "/api/v1/torrent-client-connections/qbittorrent", qbitConnBody)
	require.Equal(t, http.StatusOK, w.Code)
	var saved struct {
		Kind            string `json:"kind"`
		URL             string `json:"url"`
		Password        string `json:"password"`
		DeleteWithFiles bool   `json:"delete_with_files"`
		TimeoutSeconds  int    `json:"timeout_seconds"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&saved))
	require.Equal(t, "qbittorrent", saved.Kind)
	require.Equal(t, "http://qbit:8080", saved.URL, "trailing slash trimmed")
	require.Equal(t, "pw", saved.Password, "password is echoed back for UI editing")
	require.True(t, saved.DeleteWithFiles)
	require.Equal(t, 20, saved.TimeoutSeconds)
	require.True(t, *reloadCalled, "saving a connection reloads the registry")

	// It appears in the list now.
	w = doTorrentConn(t, h, http.MethodGet, "/api/v1/torrent-client-connections", "")
	require.NoError(t, json.NewDecoder(w.Body).Decode(&listResp))
	require.Len(t, listResp.Connections, 1)

	// Delete it.
	w = doTorrentConn(t, h, http.MethodDelete, "/api/v1/torrent-client-connections/qbittorrent", "")
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestTorrentClientConnections_PasswordCarryForward(t *testing.T) {
	h, _ := buildTorrentConnSrv(t)

	w := doTorrentConn(t, h, http.MethodPut, "/api/v1/torrent-client-connections/qbittorrent", qbitConnBody)
	require.Equal(t, http.StatusOK, w.Code)

	// Re-save with an empty password: the stored secret must be carried forward.
	w = doTorrentConn(t, h, http.MethodPut, "/api/v1/torrent-client-connections/qbittorrent",
		`{"url":"http://qbit:8080","enabled":true}`)
	require.Equal(t, http.StatusOK, w.Code)
	var saved struct {
		Password string `json:"password"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&saved))
	require.Equal(t, "pw", saved.Password, "empty password keeps the stored one")
}

func TestTorrentClientConnections_UpsertRejections(t *testing.T) {
	h, _ := buildTorrentConnSrv(t)

	t.Run("unknown kind", func(t *testing.T) {
		w := doTorrentConn(t, h, http.MethodPut, "/api/v1/torrent-client-connections/aria2", qbitConnBody)
		require.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("scaffolded kind has no backend", func(t *testing.T) {
		w := doTorrentConn(t, h, http.MethodPut, "/api/v1/torrent-client-connections/transmission", qbitConnBody)
		require.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("enabled with invalid url", func(t *testing.T) {
		w := doTorrentConn(t, h, http.MethodPut, "/api/v1/torrent-client-connections/qbittorrent",
			`{"url":"","enabled":true}`)
		require.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestTorrentClientConnections_DeleteRejections(t *testing.T) {
	h, _ := buildTorrentConnSrv(t)

	t.Run("unknown kind 400", func(t *testing.T) {
		w := doTorrentConn(t, h, http.MethodDelete, "/api/v1/torrent-client-connections/aria2", "")
		require.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("known kind with no row 404", func(t *testing.T) {
		w := doTorrentConn(t, h, http.MethodDelete, "/api/v1/torrent-client-connections/deluge", "")
		require.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestTorrentClientConnections_Test(t *testing.T) {
	h, _ := buildTorrentConnSrv(t)

	t.Run("unknown kind 400", func(t *testing.T) {
		w := doTorrentConn(t, h, http.MethodPost, "/api/v1/torrent-client-connections/test",
			`{"kind":"aria2","url":"http://h"}`)
		require.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("missing url 400", func(t *testing.T) {
		w := doTorrentConn(t, h, http.MethodPost, "/api/v1/torrent-client-connections/test",
			`{"kind":"qbittorrent","url":""}`)
		require.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("unreachable host 502", func(t *testing.T) {
		w := doTorrentConn(t, h, http.MethodPost, "/api/v1/torrent-client-connections/test",
			`{"kind":"qbittorrent","url":"http://127.0.0.1:1","timeout_seconds":1}`)
		require.Equal(t, http.StatusBadGateway, w.Code)
	})
}
