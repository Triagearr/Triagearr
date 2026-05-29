package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func doScoring(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
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

func TestScoringDefaults_GetPutRoundTrip(t *testing.T) {
	_, _, h := buildSrv(t, "")

	// GET returns the current defaults.
	w := doScoring(t, h, http.MethodGet, "/api/v1/scoring/defaults", "")
	require.Equal(t, http.StatusOK, w.Code)

	// PUT new values, then GET reflects them.
	put := doScoring(t, h, http.MethodPut, "/api/v1/scoring/defaults",
		`{"min_ratio":2.5,"min_seed_days":14,"rare_threshold":3}`)
	require.Equal(t, http.StatusNoContent, put.Code)

	w = doScoring(t, h, http.MethodGet, "/api/v1/scoring/defaults", "")
	require.Equal(t, http.StatusOK, w.Code)
	var got struct {
		MinRatio      float64 `json:"min_ratio"`
		MinSeedDays   int     `json:"min_seed_days"`
		RareThreshold int     `json:"rare_threshold"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	require.InDelta(t, 2.5, got.MinRatio, 0.001)
	require.Equal(t, 14, got.MinSeedDays)
	require.Equal(t, 3, got.RareThreshold)
}

func TestScoringDefaults_Validation(t *testing.T) {
	_, _, h := buildSrv(t, "")
	for name, body := range map[string]string{
		"negative ratio":     `{"min_ratio":-1,"min_seed_days":1,"rare_threshold":1}`,
		"negative seed days": `{"min_ratio":1,"min_seed_days":-1,"rare_threshold":1}`,
		"negative rare":      `{"min_ratio":1,"min_seed_days":1,"rare_threshold":-1}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := doScoring(t, h, http.MethodPut, "/api/v1/scoring/defaults", body)
			require.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
	t.Run("malformed JSON", func(t *testing.T) {
		w := doScoring(t, h, http.MethodPut, "/api/v1/scoring/defaults", `{not json`)
		require.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestTrackerPolicies_UpsertListDelete(t *testing.T) {
	_, _, h := buildSrv(t, "")

	// Empty initially.
	w := doScoring(t, h, http.MethodGet, "/api/v1/scoring/tracker-policies", "")
	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Empty(t, list)

	// Upsert a policy (rare_threshold null = inherit default).
	put := doScoring(t, h, http.MethodPut, "/api/v1/scoring/tracker-policies/tracker.example.org",
		`{"min_ratio":1.5,"min_seed_days":7,"rare_threshold":null,"enabled":true}`)
	require.Equal(t, http.StatusOK, put.Code)
	var saved struct {
		TrackerHost   string `json:"tracker_host"`
		RareThreshold *int   `json:"rare_threshold"`
		Enabled       bool   `json:"enabled"`
	}
	require.NoError(t, json.NewDecoder(put.Body).Decode(&saved))
	require.Equal(t, "tracker.example.org", saved.TrackerHost)
	require.Nil(t, saved.RareThreshold, "null rare_threshold means inherit default")
	require.True(t, saved.Enabled)

	// It now appears in the list.
	w = doScoring(t, h, http.MethodGet, "/api/v1/scoring/tracker-policies", "")
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	require.Len(t, list, 1)

	// Delete it.
	del := doScoring(t, h, http.MethodDelete, "/api/v1/scoring/tracker-policies/tracker.example.org", "")
	require.Equal(t, http.StatusNoContent, del.Code)
}

func TestTrackerPolicy_DeleteUnknown404(t *testing.T) {
	_, _, h := buildSrv(t, "")
	del := doScoring(t, h, http.MethodDelete, "/api/v1/scoring/tracker-policies/nope.example.org", "")
	require.Equal(t, http.StatusNotFound, del.Code)
}

func TestTrackerPolicy_InvalidValue400(t *testing.T) {
	_, _, h := buildSrv(t, "")
	w := doScoring(t, h, http.MethodPut, "/api/v1/scoring/tracker-policies/t.example.org",
		`{"min_ratio":-2,"min_seed_days":1,"enabled":true}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
