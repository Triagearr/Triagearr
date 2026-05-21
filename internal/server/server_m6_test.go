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
	vols := []decider.Volume{{
		Name: "data", Path: "/data", TargetFreePercent: 20, MaxRunSizeGB: 100,
	}}
	srv := server.New(server.Options{
		Bind:    "127.0.0.1:0",
		APIKey:  testAPIKey,
		Auth:    cfg.HTTP.Auth,
		Store:   s,
		Config:  cfg,
		Version: server.VersionInfo{Version: "test", Commit: "abc", Date: "2026-05-21"},
		Decider: decider.New(s),
		Volume: func(name string) (decider.Volume, bool) {
			for _, v := range vols {
				if v.Name == name {
					return v, true
				}
			}
			return decider.Volume{}, false
		},
		Volumes: func() []decider.Volume { return vols },
	})
	return srv.Handler()
}

func TestAuthMode_None_NoKeyRequired(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494", Auth: config.HTTPAuthNone}}
	h := buildSrvM6(t, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/summary", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestAuthMode_Apikey_RequiresHeader(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "0.0.0.0:9494", Auth: config.HTTPAuthAPIKey}}
	h := buildSrvM6(t, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/summary", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthModeEndpoint_IsUnauthenticated(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "0.0.0.0:9494", Auth: config.HTTPAuthAPIKey}}
	h := buildSrvM6(t, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth-mode", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "apikey", body["auth"])
}

func TestSecurityHeaders_Present(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494", Auth: config.HTTPAuthNone}}
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
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494", Auth: config.HTTPAuthNone}}
	h := buildSrvM6(t, cfg)

	// First call OK.
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/runs", strings.NewReader(`{"volume":"data"}`))
	req1.RemoteAddr = "10.0.0.1:1234"
	h.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code, w1.Body.String())

	// Immediate second call from same IP rejected.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/runs", strings.NewReader(`{"volume":"data"}`))
	req2.RemoteAddr = "10.0.0.1:1234"
	h.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusTooManyRequests, w2.Code)
}

func TestConfigRedaction_NoSecretLeaks(t *testing.T) {
	secrets := []string{"sk-very-secret-arr-key", "qbit-pass-9000"}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494", Auth: config.HTTPAuthNone},
		Qbit: config.QbitConfig{Enabled: true, URL: "http://qbit", Username: "u", Password: secrets[1]},
		Arrs: config.ArrsConfig{Sonarr: []config.ArrInstanceConfig{
			{Name: "primary", Enabled: true, URL: "http://sonarr", APIKey: secrets[0]},
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
		HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494", Auth: config.HTTPAuthNone},
		Volumes: []config.VolumeConfig{
			{Name: "data", Path: "/data", DiskPressure: config.DiskPressureConfig{Enabled: true, TargetFreePercent: 20, ThresholdFreePercent: 10}},
		},
	}
	h := buildSrvM6(t, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/summary", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Contains(t, body, "volumes")
	require.Contains(t, body, "counts")
	require.Contains(t, body, "last_runs")
	require.Contains(t, body, "top_score")
}

func TestTorrentsList_Filter(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494", Auth: config.HTTPAuthNone}}
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
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494", Auth: config.HTTPAuthNone}}
	h := buildSrvM6(t, cfg)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/version", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var v server.VersionInfo
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	require.Equal(t, "test", v.Version)
}
