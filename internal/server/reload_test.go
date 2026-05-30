package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/server"
)

// hnrWindow reads scoring.hnr_window_days out of GET /api/v1/settings, which
// the handler serves from the live Engine — the value the rest of the daemon
// is currently scoring against.
func hnrWindow(t *testing.T, h http.Handler) int {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/settings", nil)
	req.Header.Set("X-API-Key", testAPIKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Values struct {
			Scoring struct {
				HnRWindowDays int `json:"hnr_window_days"`
			} `json:"scoring"`
		} `json:"values"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	return body.Values.Scoring.HnRWindowDays
}

func putHnRWindow(t *testing.T, h http.Handler, days int) *httptest.ResponseRecorder {
	t.Helper()
	payload := `{"overrides":[{"key":"scoring.hnr_window_days","value":` + strconv.Itoa(days) + `}]}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/settings", strings.NewReader(payload))
	req.Header.Set("X-API-Key", testAPIKey)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestPutSettings_ReloadSwapsEngine asserts the synchronous reload contract:
// once PUT returns 200, the new config is already live (the Engine has been
// swapped), so the very next read reflects it without any settle delay.
func TestPutSettings_ReloadSwapsEngine(t *testing.T) {
	s := testStore(t)
	cfgB := &config.Config{Scoring: config.ScoringConfig{HnRWindowDays: 21}}

	var srv *server.Server
	srv = server.New(server.Options{
		Bind:           "127.0.0.1:0",
		APIKey:         testAPIKey,
		Store:          s,
		Reload:         func(context.Context) error { srv.SwapEngine(&server.Engine{Config: cfgB}); return nil },
		ReloadValidate: func([]config.Override) error { return nil },
	}, &server.Engine{Config: &config.Config{Scoring: config.ScoringConfig{HnRWindowDays: 14}}})
	h := srv.Handler()

	require.Equal(t, 14, hnrWindow(t, h))
	require.Equal(t, http.StatusOK, putHnRWindow(t, h, 21).Code)
	require.Equal(t, 21, hnrWindow(t, h), "engine should be swapped by the time PUT returns 200")
}

// TestPutSettings_ReloadFailureKeepsEngine asserts a failed reload reports 500
// and leaves the previous Engine in place — the daemon keeps serving the last
// good config rather than a half-applied one.
func TestPutSettings_ReloadFailureKeepsEngine(t *testing.T) {
	s := testStore(t)

	var srv *server.Server
	srv = server.New(server.Options{
		Bind:           "127.0.0.1:0",
		APIKey:         testAPIKey,
		Store:          s,
		Reload:         func(context.Context) error { return errors.New("preflight failed") },
		ReloadValidate: func([]config.Override) error { return nil },
	}, &server.Engine{Config: &config.Config{Scoring: config.ScoringConfig{HnRWindowDays: 14}}})
	h := srv.Handler()

	w := putHnRWindow(t, h, 21)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Equal(t, 14, hnrWindow(t, h), "engine must be unchanged after a failed reload")

	// The override was persisted before the reload ran (documented edge case):
	// the daemon stays on the old engine, but the store already holds the value.
	row, err := s.GetSettingsOverride(context.Background(), "scoring.hnr_window_days")
	require.NoError(t, err)
	require.Equal(t, "21", row.ValueJSON)
}
