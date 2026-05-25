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
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/server"
)

func buildSrvM6(t *testing.T, cfg *config.Config) http.Handler {
	t.Helper()
	s := testStore(t)
	seed(t, s)
	vol := decider.Volume{
		Name: "data", Path: "/data", TargetFreePercent: 20, MaxRunSizeGB: 100,
	}
	srv := server.New(server.Options{
		Bind:    "127.0.0.1:0",
		APIKey:  testAPIKey,
		Store:   s,
		Config:  cfg,
		Version: server.VersionInfo{Version: "test", Commit: "abc", Date: "2026-05-21"},
		Decider: decider.New(s),
		// Tight rate limits keep these tests fast and deterministic — they
		// assert the limiter engages, not specific homelab thresholds.
		RunsPerMinute: 3,
		AuthPerMinute: 3,
		Volume:        func() decider.Volume { return vol },
	})
	return srv.Handler()
}

func TestSecurityHeaders_Present(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/summary", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
	require.NotEmpty(t, w.Header().Get("Content-Security-Policy"))
	require.Equal(t, "()", w.Header().Get("Permissions-Policy"))
}

func TestRateLimit_PostRuns(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg) // RunsPerMinute=3 (see buildSrvM6)

	got429 := false
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/runs", strings.NewReader(`{}`))
		req.RemoteAddr = "10.0.0.1:1234"
		h.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			got429 = true
			break
		}
		require.Equal(t, http.StatusOK, w.Code, "call %d: %s", i+1, w.Body.String())
	}
	require.True(t, got429, "rate limiter never engaged after 10 rapid run requests")
}

func TestConfigRedaction_NoSecretLeaks(t *testing.T) {
	secrets := []string{"sk-very-secret-arr-key", "qbit-pass-9000"}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"},
		TorrentClients: config.TorrentClientsConfig{
			Qbittorrent: config.TorrentClientInstanceConfig{Enabled: true, URL: "http://qbit", Username: "u", Password: secrets[1]},
		},
		Arrs: config.ArrsConfig{Sonarr: config.ArrInstanceConfig{
			Enabled: true, URL: "http://sonarr", APIKey: secrets[0],
		}},
	}
	h := buildSrvM6(t, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/config", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	body := w.Body.String()
	for _, secret := range secrets {
		require.NotContains(t, body, secret, "redacted body must not leak %q", secret)
	}
	require.Contains(t, body, "***")
}

func TestSummaryEndpoint_Shape(t *testing.T) {
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"},
		Volume: config.VolumeConfig{
			Name: "data", Path: "/data",
			DiskPressure: config.DiskPressureConfig{Enabled: true, TargetFreePercent: 20, ThresholdFreePercent: 10},
		},
	}
	h := buildSrvM6(t, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/summary", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Contains(t, body, "volume")
	require.Contains(t, body, "counts")
	require.Contains(t, body, "last_runs")
	require.Contains(t, body, "top_score")
}

func TestTorrentsList_Filter(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/torrents?q=n1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Torrents []map[string]any `json:"torrents"`
		Total    int              `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 1, body.Total)
	require.Len(t, body.Torrents, 1)
	require.Equal(t, "n1", body.Torrents[0]["name"])
}

func TestVersionEndpoint(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/version", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var v server.VersionInfo
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	require.Equal(t, "test", v.Version)
}
