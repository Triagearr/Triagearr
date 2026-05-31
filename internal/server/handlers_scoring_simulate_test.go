package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSimulateScoring_ReturnsRankedArchetypes(t *testing.T) {
	_, _, h := buildSrv(t, "")

	body := `{
		"weights": {
			"ratio_obligation_met": 50,
			"upload_velocity_inv": 30,
			"age_days": 0.1,
			"seeders_low_guard": -1000,
			"swarm_health_bonus": 5,
			"tracker_dead_bonus": 40
		},
		"hnr_window_days": 14,
		"defaults": {"min_ratio": 1, "min_seed_days": 30, "rare_threshold": 3}
	}`

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodPost, "/api/v1/scoring/simulate", body))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var results []struct {
		Name      string `json:"name"`
		Breakdown struct {
			Score   float64 `json:"score"`
			Factors []struct {
				Name string `json:"name"`
				Gate string `json:"gate"`
			} `json:"factors"`
		} `json:"breakdown"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &results))
	require.Len(t, results, 6)

	byName := map[string]float64{}
	for _, r := range results {
		byName[r.Name] = r.Breakdown.Score
		require.Len(t, r.Breakdown.Factors, 8, "archetype %s should expose all eight factors", r.Name)
	}

	// The in-HnR-window archetype is hard-vetoed; the dead-tracker library is a
	// prime reap target. The veto must rank far below the graveyard torrent.
	require.Less(t, byName["private_in_hnr_window"], byName["dead_tracker_library"])
	require.Less(t, byName["private_in_hnr_window"], -100.0)
}

func TestSimulateScoring_EmptyBodyUsesEffectiveConfig(t *testing.T) {
	_, _, h := buildSrv(t, "")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodPost, "/api/v1/scoring/simulate", `{}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var results []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &results))
	require.Len(t, results, 6)
}
