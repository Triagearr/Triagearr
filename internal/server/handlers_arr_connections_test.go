package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestArrConnections_CreateListUpdateDelete(t *testing.T) {
	h, _, reloadCalled := buildArrConnSrv(t)

	create := `{"kind":"sonarr","name":"main","url":"http://sonarr:8989","api_key":"k1","enabled":true,"poll":true,"act":false,"timeout_seconds":30}`
	w := doArrConn(t, h, http.MethodPost, "/api/v1/arr-connections", create)
	require.Equal(t, http.StatusCreated, w.Code)
	var created struct {
		ID  int64 `json:"id"`
		Act bool  `json:"act"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))
	require.Positive(t, created.ID)
	require.True(t, *reloadCalled)

	w = doArrConn(t, h, http.MethodGet, "/api/v1/arr-connections", "")
	require.Equal(t, http.StatusOK, w.Code)
	var list struct {
		Connections []struct {
			ID  int64  `json:"id"`
			URL string `json:"url"`
		} `json:"connections"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Len(t, list.Connections, 1)

	update := `{"kind":"sonarr","name":"main","url":"http://sonarr:9999","api_key":"k1","enabled":true,"poll":true,"act":true,"timeout_seconds":45}`
	w = doArrConn(t, h, http.MethodPut, "/api/v1/arr-connections/"+itoa(created.ID), update)
	require.Equal(t, http.StatusOK, w.Code)

	w = doArrConn(t, h, http.MethodGet, "/api/v1/arr-connections", "")
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Equal(t, "http://sonarr:9999", list.Connections[0].URL)

	w = doArrConn(t, h, http.MethodDelete, "/api/v1/arr-connections/"+itoa(created.ID), "")
	require.Equal(t, http.StatusNoContent, w.Code)

	w = doArrConn(t, h, http.MethodGet, "/api/v1/arr-connections", "")
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Empty(t, list.Connections)
}

func TestArrConnections_RejectsDuplicate(t *testing.T) {
	h, _, _ := buildArrConnSrv(t)
	body := `{"kind":"sonarr","name":"main","url":"http://x:8989","api_key":"k","enabled":true,"poll":true,"act":false,"timeout_seconds":30}`
	require.Equal(t, http.StatusCreated, doArrConn(t, h, http.MethodPost, "/api/v1/arr-connections", body).Code)
	require.Equal(t, http.StatusConflict, doArrConn(t, h, http.MethodPost, "/api/v1/arr-connections", body).Code)
}

func TestArrConnections_RejectsInvalidInput(t *testing.T) {
	h, _, _ := buildArrConnSrv(t)

	badKind := `{"kind":"plex","name":"x","url":"http://x","api_key":"k","enabled":false}`
	require.Equal(t, http.StatusBadRequest, doArrConn(t, h, http.MethodPost, "/api/v1/arr-connections", badKind).Code)

	noName := `{"kind":"sonarr","name":"  ","url":"http://x","api_key":"k","enabled":false}`
	require.Equal(t, http.StatusBadRequest, doArrConn(t, h, http.MethodPost, "/api/v1/arr-connections", noName).Code)

	enabledNoKey := `{"kind":"sonarr","name":"x","url":"http://x:8989","api_key":"","enabled":true}`
	require.Equal(t, http.StatusBadRequest, doArrConn(t, h, http.MethodPost, "/api/v1/arr-connections", enabledNoKey).Code)
}

func TestArrConnections_UpdateUnknownID(t *testing.T) {
	h, _, _ := buildArrConnSrv(t)
	body := `{"kind":"sonarr","name":"x","url":"http://x:8989","api_key":"k","enabled":true,"poll":true,"act":false,"timeout_seconds":30}`
	require.Equal(t, http.StatusNotFound, doArrConn(t, h, http.MethodPut, "/api/v1/arr-connections/4242", body).Code)
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
