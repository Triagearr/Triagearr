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
	"github.com/Triagearr/Triagearr/internal/server"
	"github.com/Triagearr/Triagearr/internal/store"
)

// buildSettingsSrv mirrors buildSrvM6 but wires the ReloadValidate / Reload
// hooks so the settings handlers are fully functional.
func buildSettingsSrv(t *testing.T) (http.Handler, *store.Store, *bool) {
	t.Helper()
	s := testStore(t)
	cfg := &config.Config{
		Mode: config.ModeDryRun,
		HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"},
		Scoring: config.ScoringConfig{
			HnRWindowDays: 14,
		},
	}
	var reloadCalled bool
	srv := server.New(server.Options{
		Bind:    "127.0.0.1:0",
		APIKey:  testAPIKey,
		Store:   s,
		Version: server.VersionInfo{Version: "test"},
		Reload: func(context.Context) error {
			reloadCalled = true
			return nil
		},
		ReloadValidate: func(overrides []config.Override) error {
			// Stub: accept anything well-formed. The real implementation
			// re-loads YAML + overrides through config.LoadWithOverrides.
			for _, o := range overrides {
				var v any
				if err := json.Unmarshal([]byte(o.ValueJSON), &v); err != nil {
					return err
				}
			}
			return nil
		},
	}, &server.Engine{Config: cfg})
	return srv.Handler(), s, &reloadCalled
}

func TestGetSettings_ReturnsValuesAndOverrides(t *testing.T) {
	h, s, _ := buildSettingsSrv(t)
	require.NoError(t, s.UpsertSettingsOverride(context.Background(), "scoring.hnr_window_days", `21`))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/settings", nil)
	req.Header.Set("X-API-Key", testAPIKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Values struct {
			Mode    string `json:"mode"`
			Scoring struct {
				HnRWindowDays int `json:"hnr_window_days"`
			} `json:"scoring"`
		} `json:"values"`
		OverriddenKeys []string `json:"overridden_keys"`
		Editable       []string `json:"editable_prefixes"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	require.Equal(t, "dry-run", body.Values.Mode)
	require.Equal(t, 14, body.Values.Scoring.HnRWindowDays)
	require.Contains(t, body.OverriddenKeys, "scoring.hnr_window_days")
	require.Contains(t, body.Editable, "scoring")
	require.Contains(t, body.Editable, "mode")
}

func TestGetSettings_NotificationsAllProviders(t *testing.T) {
	h, _, _ := buildSettingsSrv(t)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/settings", nil)
	req.Header.Set("X-API-Key", testAPIKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Values struct {
			Notifications map[string]json.RawMessage `json:"notifications"`
		} `json:"values"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	for _, p := range []string{"telegram", "discord", "ntfy", "email", "slack", "webhook", "target_unreachable"} {
		require.Contains(t, body.Values.Notifications, p, "notifications DTO missing %q", p)
	}
	// Routing keys flatten into each provider object.
	var tg struct {
		MinSeverity string   `json:"min_severity"`
		Mute        []string `json:"mute"`
	}
	require.NoError(t, json.Unmarshal(body.Values.Notifications["telegram"], &tg))
}

func TestNotificationCatalogue(t *testing.T) {
	h, _, _ := buildSettingsSrv(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/notifications/catalogue", nil)
	req.Header.Set("X-API-Key", testAPIKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var cat []struct {
		Kind     string `json:"kind"`
		Severity string `json:"severity"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&cat))
	require.NotEmpty(t, cat)
	kinds := map[string]string{}
	for _, e := range cat {
		kinds[e.Kind] = e.Severity
	}
	require.Equal(t, "warning", kinds["disk.target_unreachable"])
	require.Equal(t, "error", kinds["run.failed"])
}

func TestNotificationDeliveries_EmptyByDefault(t *testing.T) {
	h, _, _ := buildSettingsSrv(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/notifications/deliveries", nil)
	req.Header.Set("X-API-Key", testAPIKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var dels []map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&dels))
	require.Empty(t, dels)
}

func TestTestNotification_NoProviderEnabled(t *testing.T) {
	h, _, _ := buildSettingsSrv(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/notifications/test", nil)
	req.Header.Set("X-API-Key", testAPIKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	// No notifier wired into the test engine → 400.
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPutSettings_PersistsAndReloads(t *testing.T) {
	h, s, reloadCalled := buildSettingsSrv(t)

	payload := `{"overrides":[{"key":"scoring.hnr_window_days","value":21}]}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/settings", strings.NewReader(payload))
	req.Header.Set("X-API-Key", testAPIKey)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	row, err := s.GetSettingsOverride(context.Background(), "scoring.hnr_window_days")
	require.NoError(t, err)
	require.Equal(t, "21", row.ValueJSON)
	require.True(t, *reloadCalled)
}

func TestPutSettings_RejectsForbiddenKey(t *testing.T) {
	h, _, _ := buildSettingsSrv(t)
	payload := `{"overrides":[{"key":"http.bind","value":"0.0.0.0:1234"}]}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/settings", strings.NewReader(payload))
	req.Header.Set("X-API-Key", testAPIKey)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestPutSettings_ModeIsEditable(t *testing.T) {
	h, s, reloadCalled := buildSettingsSrv(t)

	payload := `{"overrides":[{"key":"mode","value":"live"}]}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/settings", strings.NewReader(payload))
	req.Header.Set("X-API-Key", testAPIKey)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	row, err := s.GetSettingsOverride(context.Background(), "mode")
	require.NoError(t, err)
	require.Equal(t, `"live"`, row.ValueJSON)
	require.True(t, *reloadCalled)
}

func TestPutSettings_RejectsAPIKey(t *testing.T) {
	h, _, _ := buildSettingsSrv(t)
	payload := `{"overrides":[{"key":"arrs.sonarr.0.api_key","value":"new-key"}]}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/settings", strings.NewReader(payload))
	req.Header.Set("X-API-Key", testAPIKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestPutSettings_DeletesWhenValueNull(t *testing.T) {
	h, s, _ := buildSettingsSrv(t)
	require.NoError(t, s.UpsertSettingsOverride(context.Background(), "scoring.hnr_window_days", `21`))

	payload := `{"overrides":[{"key":"scoring.hnr_window_days","value":null}]}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/settings", strings.NewReader(payload))
	req.Header.Set("X-API-Key", testAPIKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	rows, err := s.ListSettingsOverrides(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestDeleteSetting_RevertsToDefault(t *testing.T) {
	h, s, reloadCalled := buildSettingsSrv(t)
	require.NoError(t, s.UpsertSettingsOverride(context.Background(), "polling.torrent_client_interval", `"5m"`))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/v1/settings/polling.torrent_client_interval", nil)
	req.Header.Set("X-API-Key", testAPIKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, *reloadCalled)

	rows, err := s.ListSettingsOverrides(context.Background())
	require.NoError(t, err)
	require.Empty(t, rows)
}
