package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSummary_IncludesTorrentClients(t *testing.T) {
	_, s, h := buildSrv(t, "")
	require.NoError(t, s.UpsertTorrentClientInstance(
		context.Background(), "qbittorrent", "http://qbit:8080", true, ""))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodGet, "/api/v1/summary", ""))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		TorrentClients []struct {
			Kind    string `json:"kind"`
			URL     string `json:"url"`
			Healthy bool   `json:"healthy"`
		} `json:"torrent_clients"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.TorrentClients, 1)
	require.Equal(t, "qbittorrent", body.TorrentClients[0].Kind)
	require.True(t, body.TorrentClients[0].Healthy)
}
